package runtime

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/degoke/health-ai-stack/pkg/jobs"
	"github.com/degoke/health-ai-stack/pkg/postgres"
	"github.com/degoke/health-ai-stack/pkg/search"
	"github.com/degoke/health-ai-stack/pkg/sqlite"
	hasync "github.com/degoke/health-ai-stack/pkg/sync"
)

const jobPollInterval = time.Second

// Runtime owns composed services, optional HTTP serving, and background workers.
type Runtime struct {
	mode     Mode
	config   Config
	services *ServiceContainer
	handler  http.Handler

	sqliteDB   *sqlite.DB
	postgresDB *postgres.DB

	httpServer *http.Server
	httpAddr   net.Addr

	jobRunner     *jobs.Runner
	syncProcessor *hasync.JobProcessor
	reindexWorker *search.ReindexWorker
	syncEngine    *hasync.Engine

	jobCtx    context.Context
	jobCancel context.CancelFunc
	jobWG     sync.WaitGroup

	cleanup cleanupStack

	mu           sync.Mutex
	started      bool
	starting     bool
	shutdown     bool
	shutdownDone chan struct{}
	shutdownErr  error

	backgroundErr error
}

// Build constructs a wired runtime from the builder configuration.
func (b *Builder) Build(ctx context.Context) (*Runtime, error) {
	rt := &Runtime{}
	if err := b.wire(ctx, rt); err != nil {
		return nil, err
	}
	return rt, nil
}

// Mode returns the effective deployment mode.
func (rt *Runtime) Mode() Mode {
	return rt.mode
}

// Config returns the normalized runtime configuration.
func (rt *Runtime) Config() Config {
	return rt.config
}

// Services exposes the wired service graph.
func (rt *Runtime) Services() *ServiceContainer {
	return rt.services
}

// Handler returns the FHIR HTTP handler for embedding or testing.
func (rt *Runtime) Handler() http.Handler {
	return rt.handler
}

// IsStarted reports whether the managed runtime has completed startup and is
// accepting requests. It is used by the readiness probe.
func (rt *Runtime) IsStarted() bool {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	return rt.started
}

// Start begins background workers and the managed HTTP server when configured.
func (rt *Runtime) Start(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	rt.mu.Lock()
	if rt.shutdown {
		rt.mu.Unlock()
		return fmt.Errorf("runtime: cannot start after shutdown")
	}
	if rt.started || rt.starting {
		rt.mu.Unlock()
		return ErrAlreadyStarted
	}
	rt.starting = true

	if rt.jobRunner != nil || rt.syncProcessor != nil {
		rt.jobCtx, rt.jobCancel = context.WithCancel(ctx)
		rt.jobWG.Add(1)
		go func() {
			defer rt.jobWG.Done()
			rt.runJobLoop(rt.jobCtx)
		}()
	}

	if rt.config.HTTPAddr != "" {
		server := &http.Server{
			Addr:    rt.config.HTTPAddr,
			Handler: rt.handler,
		}
		rt.httpServer = server

		ln, err := net.Listen("tcp", rt.config.HTTPAddr)
		if err != nil {
			jobCancel := rt.jobCancel
			rt.mu.Unlock()
			if jobCancel != nil {
				jobCancel()
				rt.jobWG.Wait()
			}
			rt.mu.Lock()
			rt.jobCtx = nil
			rt.jobCancel = nil
			rt.httpServer = nil
			rt.httpAddr = nil
			rt.starting = false
			rt.mu.Unlock()
			return fmt.Errorf("runtime: listen %s: %w", rt.config.HTTPAddr, err)
		}
		rt.httpAddr = ln.Addr()

		rt.jobWG.Add(1)
		go func() {
			defer rt.jobWG.Done()
			if err := server.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
				rt.recordBackgroundError(fmt.Errorf("%w: http serve: %v", ErrBackgroundWorker, err))
			}
		}()
	}

	rt.started = true
	rt.starting = false
	rt.mu.Unlock()
	return nil
}

// Shutdown stops HTTP serving, background workers, and closes persistence backends.
// Repeated calls are safe.
func (rt *Runtime) Shutdown(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}

	rt.mu.Lock()
	if rt.shutdown {
		done := rt.shutdownDone
		rt.mu.Unlock()
		if done == nil {
			return nil
		}
		select {
		case <-done:
			rt.mu.Lock()
			err := rt.shutdownErr
			rt.mu.Unlock()
			return err
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	rt.shutdown = true
	rt.started = false
	rt.starting = false
	rt.shutdownDone = make(chan struct{})

	httpServer := rt.httpServer
	jobCancel := rt.jobCancel
	done := rt.shutdownDone
	rt.mu.Unlock()

	var shutdownErr error
	if httpServer != nil {
		if err := httpServer.Shutdown(ctx); err != nil && !errors.Is(err, http.ErrServerClosed) {
			shutdownErr = errors.Join(shutdownErr, fmt.Errorf("runtime: http shutdown: %w", err))
			// Shutdown may leave active connections alive after the caller's
			// deadline. Force-close them so lifecycle cleanup can complete.
			if closeErr := httpServer.Close(); closeErr != nil && !errors.Is(closeErr, http.ErrServerClosed) {
				shutdownErr = errors.Join(shutdownErr, fmt.Errorf("runtime: force-close HTTP server: %w", closeErr))
			}
		}
	}

	if jobCancel != nil {
		jobCancel()
	}

	// Workers are owned by the runtime and must finish before their stores are
	// closed. The worker context is cancelled above; wait independently of the
	// caller's deadline so a timed-out Shutdown cannot strand open resources.
	rt.jobWG.Wait()

	shutdownErr = errors.Join(shutdownErr, rt.closeAdapters(context.Background()))
	rt.cleanup.run()
	shutdownErr = errors.Join(shutdownErr, rt.BackgroundError())

	rt.mu.Lock()
	rt.shutdownErr = shutdownErr
	close(done)
	rt.mu.Unlock()
	return shutdownErr
}

func (rt *Runtime) runJobLoop(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		processed := false
		if rt.jobRunner != nil {
			ok, err := rt.jobRunner.RunOnce(ctx)
			if err != nil && !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
				rt.recordBackgroundError(fmt.Errorf("%w: jobs: %v", ErrBackgroundWorker, err))
			}
			processed = processed || ok
		}
		if rt.syncProcessor != nil {
			ok, err := rt.syncProcessor.ProcessNext(ctx)
			if err != nil && !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
				rt.recordBackgroundError(fmt.Errorf("%w: sync jobs: %v", ErrBackgroundWorker, err))
			}
			processed = processed || ok
		}

		if !processed {
			timer := time.NewTimer(jobPollInterval)
			select {
			case <-ctx.Done():
				timer.Stop()
				return
			case <-timer.C:
			}
		}
	}
}

// BackgroundError returns the first background worker or managed server error observed.
func (rt *Runtime) BackgroundError() error {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	return rt.backgroundErr
}

func (rt *Runtime) recordBackgroundError(err error) {
	if err == nil {
		return
	}
	rt.mu.Lock()
	defer rt.mu.Unlock()
	if rt.backgroundErr == nil {
		rt.backgroundErr = err
	}
}

func (rt *Runtime) closeAdapters(ctx context.Context) error {
	if rt.services == nil {
		return nil
	}
	var closeErr error
	for _, adapter := range []interface{}{
		rt.services.BlobStore,
		rt.services.ExternalSearch,
		rt.services.Warehouse,
	} {
		if c, ok := adapter.(CloseableAdapter); ok && c != nil {
			closeErr = errors.Join(closeErr, c.Close(ctx))
		}
	}
	return closeErr
}

// HTTPAddr returns the bound listen address after Start when HTTP is configured.
func (rt *Runtime) HTTPAddr() net.Addr {
	return rt.httpAddr
}

// SyncEngine returns the configured sync engine, if any.
func (rt *Runtime) SyncEngine() *hasync.Engine {
	return rt.syncEngine
}

// ReindexWorker returns the configured search reindex worker, if any.
func (rt *Runtime) ReindexWorker() *search.ReindexWorker {
	return rt.reindexWorker
}
