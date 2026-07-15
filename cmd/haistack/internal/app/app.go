// Package app wires haistack CLI commands to pkg/runtime and related services.
package app

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/degoke/health-ai-stack/cmd/haistack/internal/config"
	"github.com/degoke/health-ai-stack/pkg/core"
	"github.com/degoke/health-ai-stack/pkg/fhirpath"
	"github.com/degoke/health-ai-stack/pkg/runtime"
	"github.com/degoke/health-ai-stack/pkg/search"
	"github.com/degoke/health-ai-stack/pkg/store"
	"github.com/degoke/health-ai-stack/pkg/types"
)

// OutputFormat selects human or machine-readable command output.
type OutputFormat string

const (
	OutputText OutputFormat = "text"
	OutputJSON OutputFormat = "json"
)

// Printer writes command results in the selected format.
type Printer struct {
	Format OutputFormat
	Out    io.Writer
	Err    io.Writer
}

// NewPrinter returns a printer writing to stdout/stderr by default.
func NewPrinter(format OutputFormat) *Printer {
	if format == "" {
		format = OutputText
	}
	return &Printer{Format: format, Out: os.Stdout, Err: os.Stderr}
}

// Print writes a value as text or JSON.
func (p *Printer) Print(v any) error {
	if p.Format == OutputJSON {
		enc := json.NewEncoder(p.Out)
		enc.SetIndent("", "  ")
		return enc.Encode(v)
	}
	return printText(p.Out, v)
}

// Println writes a plain text line (ignored for JSON-only flows).
func (p *Printer) Println(line string) error {
	if p.Format == OutputJSON {
		return nil
	}
	_, err := fmt.Fprintln(p.Out, line)
	return err
}

// Session owns a wired runtime for one-shot CLI commands.
type Session struct {
	Config  config.Config
	Runtime *runtime.Runtime
}

// OpenSession builds a runtime from configuration without starting HTTP.
func OpenSession(ctx context.Context, cfg config.Config) (*Session, error) {
	rt, err := BuildRuntime(ctx, cfg, "")
	if err != nil {
		return nil, err
	}
	return &Session{Config: cfg, Runtime: rt}, nil
}

// Close shuts down the underlying runtime.
func (s *Session) Close(ctx context.Context) error {
	if s == nil || s.Runtime == nil {
		return nil
	}
	return s.Runtime.Shutdown(ctx)
}

// BuildRuntime constructs a runtime from CLI config. httpAddr is optional.
func BuildRuntime(ctx context.Context, cfg config.Config, httpAddr string) (*runtime.Runtime, error) {
	b := runtime.New()
	switch cfg.Storage.Driver {
	case config.DriverPostgres:
		b.WithPostgresAllInOne(cfg.Storage.PostgresDSN, cfg.Storage.TenantID)
	default:
		b.WithSQLite(cfg.Storage.SQLitePath)
	}
	if cfg.Runtime.EnableSearch {
		b.WithSearch()
	}
	if len(cfg.Runtime.ModulePaths) > 0 {
		b.WithModules(cfg.Runtime.ModulePaths...)
	}
	if cfg.Sync.HubURL != "" {
		b.WithSync(cfg.Sync.HubURL).WithSyncNode(cfg.Sync.NodeID)
	}
	if httpAddr != "" {
		b.WithHTTP(httpAddr)
	}
	return b.Build(ctx)
}

// ReadResourceFile parses one JSON resource file into an envelope.
func ReadResourceFile(path string) (*types.ResourceEnvelope, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read resource file: %w", err)
	}
	return types.NewJSONCodec().ParseJSON("", data)
}

// UpsertResource creates or updates a resource by existence check.
func UpsertResource(ctx context.Context, svc *core.ResourceService, env *types.ResourceEnvelope) (action string, result *types.ResourceEnvelope, err error) {
	if env.ID != "" {
		_, readErr := svc.Read(ctx, env.ResourceType, env.ID)
		if readErr == nil {
			updated, err := svc.Update(ctx, env)
			return "update", updated, err
		}
		if !core.IsNotFound(readErr) {
			return "", nil, readErr
		}
	}
	created, err := svc.Create(ctx, env)
	return "create", created, err
}

// ParseSearchParams parses repeated key=value CLI arguments into url.Values-like map.
func ParseSearchParams(args []string) (map[string][]string, error) {
	out := make(map[string][]string)
	for _, arg := range args {
		key, value, ok := splitPair(arg)
		if !ok {
			return nil, fmt.Errorf("invalid search parameter %q, expected key=value", arg)
		}
		out[key] = append(out[key], value)
	}
	return out, nil
}

func splitPair(arg string) (string, string, bool) {
	for i := 0; i < len(arg); i++ {
		if arg[i] == '=' {
			if i == 0 || i == len(arg)-1 {
				return "", "", false
			}
			return arg[:i], arg[i+1:], true
		}
	}
	return "", "", false
}

// NewReindexWorker builds a synchronous reindex worker for CLI use.
func (s *Session) NewReindexWorker() (*search.ReindexWorker, error) {
	if !s.Config.Runtime.EnableSearch {
		return nil, fmt.Errorf("search is not enabled in configuration")
	}
	services := s.Runtime.Services()
	if services == nil || services.RegistrySnapshot == nil {
		return nil, fmt.Errorf("registry snapshot is not available")
	}
	engine := services.FHIRPathEngine
	if engine == nil {
		var err error
		engine, err = fhirpath.NewEngine(fhirpath.Config{})
		if err != nil {
			return nil, err
		}
	}
	reg := search.NewSnapshotRegistry(services.RegistrySnapshot)
	indexer, err := search.NewRegistryIndexer(search.RegistryIndexerConfig{
		Registry: reg,
		Engine:   engine,
	})
	if err != nil {
		return nil, err
	}
	resources, searchStore, err := s.resourceStores()
	if err != nil {
		return nil, err
	}
	if rtWorker := s.Runtime.ReindexWorker(); rtWorker != nil {
		return rtWorker, nil
	}
	return &search.ReindexWorker{
		Registry:  reg,
		Indexer:   indexer,
		Resources: resources,
		Search:    searchStore,
	}, nil
}

func (s *Session) resourceStores() (store.ResourceStore, store.SearchStore, error) {
	p := s.Runtime.Persistence()
	switch {
	case p.SQLite != nil:
		return p.SQLite.ResourceStore(), p.SQLite.SearchStore(), nil
	case p.TenantDB != nil:
		return p.TenantDB.ResourceStore(), p.TenantDB.SearchStore(), nil
	default:
		return nil, nil, fmt.Errorf("no persistence backend available")
	}
}

func printText(w io.Writer, v any) error {
	switch val := v.(type) {
	case string:
		_, err := fmt.Fprintln(w, val)
		return err
	case fmt.Stringer:
		_, err := fmt.Fprintln(w, val.String())
		return err
	default:
		data, err := json.MarshalIndent(v, "", "  ")
		if err != nil {
			return err
		}
		_, err = fmt.Fprintln(w, string(data))
		return err
	}
}
