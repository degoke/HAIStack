package command

import (
	"fmt"
	"strconv"

	"github.com/degoke/health-ai-stack/cmd/haistack/internal/app"
	"github.com/degoke/health-ai-stack/cmd/haistack/internal/config"
	"github.com/spf13/cobra"
)

// Options carries persistent CLI flags shared by all commands.
type Options struct {
	ConfigPath    string
	Output        app.OutputFormat
	StorageDriver string
	SQLitePath    string
	PostgresDSN   string
	TenantID      string
	HTTPAddr      string
	EnableSearch  string
	ModulePaths   []string
	SyncHubURL    string
	SyncNodeID    string
	Force         bool
}

// NewRootCommand builds the haistack root command tree.
func NewRootCommand() *cobra.Command {
	opts := &Options{}
	printer := app.NewPrinter(app.OutputText)

	root := &cobra.Command{
		Use:   "haistack",
		Short: "Health AI Stack developer and operator CLI",
		Long: `haistack is the command-line interface for local development and operations
against a Health AI Stack runtime. Configure storage and capabilities in haistack.yaml.`,
		SilenceUsage:  true,
		SilenceErrors: true,
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			printer.Format = opts.Output
			printer.Out = cmd.OutOrStdout()
			printer.Err = cmd.ErrOrStderr()
			return nil
		},
	}

	root.PersistentFlags().StringVar(&opts.ConfigPath, "config", config.DefaultConfigFile, "Path to haistack.yaml")
	root.PersistentFlags().StringVar((*string)(&opts.Output), "output", string(app.OutputText), "Output format: text or json")
	root.PersistentFlags().StringVar(&opts.StorageDriver, "storage-driver", "", "Storage driver override (sqlite or postgres)")
	root.PersistentFlags().StringVar(&opts.SQLitePath, "sqlite-path", "", "SQLite database path override")
	root.PersistentFlags().StringVar(&opts.PostgresDSN, "postgres-dsn", "", "Postgres DSN override")
	root.PersistentFlags().StringVar(&opts.TenantID, "tenant-id", "", "Postgres tenant ID override")
	root.PersistentFlags().StringVar(&opts.HTTPAddr, "http-addr", "", "HTTP listen address override")
	root.PersistentFlags().StringVar(&opts.EnableSearch, "enable-search", "", "Override enableSearch (true or false)")
	root.PersistentFlags().StringSliceVar(&opts.ModulePaths, "module-path", nil, "Module path override (repeatable)")
	root.PersistentFlags().StringVar(&opts.SyncHubURL, "sync-hub-url", "", "Sync hub URL override")
	root.PersistentFlags().StringVar(&opts.SyncNodeID, "sync-node-id", "", "Sync node ID override")

	root.AddCommand(
		newInitCommand(opts, printer),
		newServeCommand(opts, printer),
		newValidateCommand(opts, printer),
		newImportCommand(opts, printer),
		newSearchCommand(opts, printer),
		newFHIRPathCommand(opts, printer),
		newSyncCommand(opts, printer),
		newModuleCommand(opts, printer),
		newReindexCommand(opts, printer),
	)

	return root
}

func (o *Options) overrides() config.Overrides {
	ov := config.Overrides{ConfigPath: o.ConfigPath}
	if o.StorageDriver != "" {
		ov.StorageDriver = &o.StorageDriver
	}
	if o.SQLitePath != "" {
		ov.SQLitePath = &o.SQLitePath
	}
	if o.PostgresDSN != "" {
		ov.PostgresDSN = &o.PostgresDSN
	}
	if o.TenantID != "" {
		ov.TenantID = &o.TenantID
	}
	if o.HTTPAddr != "" {
		ov.HTTPAddr = &o.HTTPAddr
	}
	if o.EnableSearch != "" {
		if parsed, err := strconv.ParseBool(o.EnableSearch); err == nil {
			ov.EnableSearch = &parsed
		}
	}
	if len(o.ModulePaths) > 0 {
		ov.ModulePaths = &o.ModulePaths
	}
	if o.SyncHubURL != "" {
		ov.SyncHubURL = &o.SyncHubURL
	}
	if o.SyncNodeID != "" {
		ov.SyncNodeID = &o.SyncNodeID
	}
	return ov
}

func (o *Options) loadConfig() (config.Config, error) {
	path := o.ConfigPath
	if path == "" {
		path = config.DefaultConfigFile
	}
	return config.Load(path, o.overrides())
}

func exitErr(printer *app.Printer, err error) error {
	if err == nil {
		return nil
	}
	if printer.Format == app.OutputJSON {
		_ = printer.Print(map[string]string{"error": err.Error()})
	} else {
		_, _ = fmt.Fprintln(printer.Err, err)
	}
	return err
}

func writeStdout(printer *app.Printer, text string) {
	if printer.Format == app.OutputJSON {
		return
	}
	_, _ = fmt.Fprintln(printer.Out, text)
}
