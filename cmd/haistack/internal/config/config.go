package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

const (
	DefaultConfigFile = "haistack.yaml"
	DefaultSQLitePath = ".haistack/haistack.db"
	DefaultHTTPAddr   = "127.0.0.1:8080"
	DefaultSyncNodeID = "runtime-node"
	DefaultCoreModule = "modules/core"
	DriverSQLite      = "sqlite"
	DriverPostgres    = "postgres"
)

// Config is the top-level haistack CLI configuration.
type Config struct {
	Storage StorageConfig `yaml:"storage"`
	Runtime RuntimeConfig `yaml:"runtime"`
	Sync    SyncConfig    `yaml:"sync"`
}

// StorageConfig selects the persistence backend.
type StorageConfig struct {
	Driver      string `yaml:"driver"`
	SQLitePath  string `yaml:"sqlitePath"`
	PostgresDSN string `yaml:"postgresDSN"`
	TenantID    string `yaml:"tenantID"`
}

// RuntimeConfig controls local runtime capabilities.
type RuntimeConfig struct {
	HTTPAddr     string   `yaml:"httpAddr"`
	EnableSearch bool     `yaml:"enableSearch"`
	ModulePaths  []string `yaml:"modulePaths"`
}

// SyncConfig configures device-to-hub synchronization.
type SyncConfig struct {
	HubURL string `yaml:"hubURL"`
	NodeID string `yaml:"nodeID"`
}

// Defaults returns a new config populated with CLI defaults.
func Defaults() Config {
	return Config{
		Storage: StorageConfig{
			Driver:     DriverSQLite,
			SQLitePath: DefaultSQLitePath,
		},
		Runtime: RuntimeConfig{
			HTTPAddr:     DefaultHTTPAddr,
			EnableSearch: true,
			ModulePaths:  []string{DefaultCoreModule},
		},
		Sync: SyncConfig{
			NodeID: DefaultSyncNodeID,
		},
	}
}

// Validate checks required fields for the selected driver.
func (c Config) Validate() error {
	switch c.Storage.Driver {
	case DriverSQLite, "":
		if strings.TrimSpace(c.Storage.SQLitePath) == "" {
			return fmt.Errorf("storage.sqlitePath is required")
		}
	case DriverPostgres:
		if strings.TrimSpace(c.Storage.PostgresDSN) == "" {
			return fmt.Errorf("storage.postgresDSN is required for postgres driver")
		}
		if strings.TrimSpace(c.Storage.TenantID) == "" {
			return fmt.Errorf("storage.tenantID is required for postgres driver")
		}
	default:
		return fmt.Errorf("unsupported storage driver %q", c.Storage.Driver)
	}
	if c.Sync.HubURL != "" && strings.TrimSpace(c.Sync.NodeID) == "" {
		return fmt.Errorf("sync.nodeID is required when sync.hubURL is set")
	}
	return nil
}

// Normalize fills empty driver values and copies slices.
func (c *Config) Normalize() {
	if c.Storage.Driver == "" {
		c.Storage.Driver = DriverSQLite
	}
	if c.Sync.NodeID == "" {
		c.Sync.NodeID = DefaultSyncNodeID
	}
	if c.Runtime.ModulePaths == nil {
		c.Runtime.ModulePaths = []string{}
	} else {
		c.Runtime.ModulePaths = append([]string(nil), c.Runtime.ModulePaths...)
	}
}

// Overrides carries flag and environment overrides applied after file load.
type Overrides struct {
	ConfigPath string

	StorageDriver *string
	SQLitePath    *string
	PostgresDSN   *string
	TenantID      *string
	HTTPAddr      *string
	EnableSearch  *bool
	ModulePaths   *[]string
	SyncHubURL    *string
	SyncNodeID    *string
}

// Load reads configuration from path, applies defaults, env, and overrides.
func Load(path string, overrides Overrides) (Config, error) {
	cfg := Defaults()
	if path != "" {
		data, err := os.ReadFile(path)
		if err != nil {
			if !errors.Is(err, os.ErrNotExist) || !canUseDefaultsWithoutFile(path) {
				return Config{}, fmt.Errorf("read config %s: %w", path, err)
			}
		} else {
			if err := yaml.Unmarshal(data, &cfg); err != nil {
				return Config{}, fmt.Errorf("parse config %s: %w", path, err)
			}
			resolveFileRelativePaths(&cfg, path)
		}
	}
	cfg.Normalize()
	applyEnv(&cfg)
	applyOverrides(&cfg, overrides)
	cfg.Normalize()
	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func canUseDefaultsWithoutFile(path string) bool {
	clean := filepath.Clean(path)
	return clean == DefaultConfigFile || filepath.Base(clean) == DefaultConfigFile
}

func resolveFileRelativePaths(cfg *Config, path string) {
	baseDir := filepath.Dir(path)
	if cfg.Storage.SQLitePath != "" && !filepath.IsAbs(cfg.Storage.SQLitePath) {
		cfg.Storage.SQLitePath = filepath.Join(baseDir, cfg.Storage.SQLitePath)
	}
	for i, modulePath := range cfg.Runtime.ModulePaths {
		if modulePath == "" || filepath.IsAbs(modulePath) {
			continue
		}
		cfg.Runtime.ModulePaths[i] = filepath.Join(baseDir, modulePath)
	}
}

// StarterYAML returns the default haistack.yaml contents for haistack init.
func StarterYAML() []byte {
	return []byte(`storage:
  driver: sqlite
  sqlitePath: .haistack/haistack.db
  postgresDSN: ""
  tenantID: ""
runtime:
  httpAddr: 127.0.0.1:8080
  enableSearch: true
  modulePaths:
    - modules/core
sync:
  hubURL: ""
  nodeID: runtime-node
`)
}

func applyEnv(cfg *Config) {
	if v := os.Getenv("HAISTACK_STORAGE_DRIVER"); v != "" {
		cfg.Storage.Driver = v
	}
	if v := os.Getenv("HAISTACK_SQLITE_PATH"); v != "" {
		cfg.Storage.SQLitePath = v
	}
	if v := os.Getenv("HAISTACK_POSTGRES_DSN"); v != "" {
		cfg.Storage.PostgresDSN = v
	}
	if v := os.Getenv("HAISTACK_TENANT_ID"); v != "" {
		cfg.Storage.TenantID = v
	}
	if v := os.Getenv("HAISTACK_HTTP_ADDR"); v != "" {
		cfg.Runtime.HTTPAddr = v
	}
	if v := os.Getenv("HAISTACK_ENABLE_SEARCH"); v != "" {
		if parsed, err := strconv.ParseBool(v); err == nil {
			cfg.Runtime.EnableSearch = parsed
		}
	}
	if v := os.Getenv("HAISTACK_MODULE_PATHS"); v != "" {
		cfg.Runtime.ModulePaths = splitList(v)
	}
	if v := os.Getenv("HAISTACK_SYNC_HUB_URL"); v != "" {
		cfg.Sync.HubURL = v
	}
	if v := os.Getenv("HAISTACK_SYNC_NODE_ID"); v != "" {
		cfg.Sync.NodeID = v
	}
}

func applyOverrides(cfg *Config, o Overrides) {
	if o.StorageDriver != nil {
		cfg.Storage.Driver = *o.StorageDriver
	}
	if o.SQLitePath != nil {
		cfg.Storage.SQLitePath = *o.SQLitePath
	}
	if o.PostgresDSN != nil {
		cfg.Storage.PostgresDSN = *o.PostgresDSN
	}
	if o.TenantID != nil {
		cfg.Storage.TenantID = *o.TenantID
	}
	if o.HTTPAddr != nil {
		cfg.Runtime.HTTPAddr = *o.HTTPAddr
	}
	if o.EnableSearch != nil {
		cfg.Runtime.EnableSearch = *o.EnableSearch
	}
	if o.ModulePaths != nil {
		cfg.Runtime.ModulePaths = append([]string(nil), (*o.ModulePaths)...)
	}
	if o.SyncHubURL != nil {
		cfg.Sync.HubURL = *o.SyncHubURL
	}
	if o.SyncNodeID != nil {
		cfg.Sync.NodeID = *o.SyncNodeID
	}
}

func splitList(value string) []string {
	parts := strings.Split(value, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}
