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

	mu       sync.Mutex
	started  bool
	shutdown bool

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

// Start begins background workers and the managed HTTP server when configured.
func (rt *Runtime) Start(ctx context.Context) error {
	rt.mu.Lock()
	defer rt.mu.Unlock()

	if rt.shutdown {
		return fmt.Errorf("runtime: cannot start after shutdown")
	}
	if rt.started {
		return ErrAlreadyStarted
	}

	if rt.jobRunner != nil || rt.syncProcessor != nil {
		rt.jobCtx, rt.jobCancel = context.WithCancel(context.Background())
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
			rt.rollbackStart()
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
	return nil
}

// Shutdown stops HTTP serving, background workers, and closes persistence backends.
// Repeated calls are safe.
func (rt *Runtime) Shutdown(ctx context.Context) error {
	rt.mu.Lock()
	if rt.shutdown {
		rt.mu.Unlock()
		return nil
	}
	rt.shutdown = true
	rt.started = false

	httpServer := rt.httpServer
	jobCancel := rt.jobCancel
	rt.mu.Unlock()

	if httpServer != nil {
		if err := httpServer.Shutdown(ctx); err != nil && !errors.Is(err, http.ErrServerClosed) {
			return fmt.Errorf("runtime: http shutdown: %w", err)
		}
	}

	if jobCancel != nil {
		jobCancel()
	}

	done := make(chan struct{})
	go func() {
		rt.jobWG.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-ctx.Done():
		return ctx.Err()
	}

	rt.closeAdapters(ctx)
	rt.cleanup.run()
	return rt.BackgroundError()
}

func (rt *Runtime) rollbackStart() {
	if rt.jobCancel != nil {
		rt.jobCancel()
		rt.jobWG.Wait()
		rt.jobCancel = nil
	}
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
			if err != nil {
				rt.recordBackgroundError(fmt.Errorf("%w: jobs: %v", ErrBackgroundWorker, err))
			}
			processed = processed || ok
		}
		if rt.syncProcessor != nil {
			ok, err := rt.syncProcessor.ProcessNext(ctx)
			if err != nil {
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

func (rt *Runtime) closeAdapters(ctx context.Context) {
	if rt.services == nil {
		return
	}
	for _, adapter := range []interface{}{
		rt.services.BlobStore,
		rt.services.ExternalSearch,
		rt.services.Warehouse,
	} {
		if c, ok := adapter.(CloseableAdapter); ok && c != nil {
			_ = c.Close(ctx)
		}
	}
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
