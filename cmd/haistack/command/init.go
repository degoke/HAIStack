package command

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/degoke/health-ai-stack/cmd/haistack/internal/app"
	"github.com/degoke/health-ai-stack/cmd/haistack/internal/config"
	"github.com/spf13/cobra"
)

func newInitCommand(opts *Options, printer *app.Printer) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "init",
		Short: "Create a starter haistack.yaml and .haistack directory",
		Long: `Initialize a local haistack workspace by writing haistack.yaml with SQLite defaults
and creating the .haistack data directory when absent.`,
		Example: `  haistack init
  haistack init --force`,
		RunE: func(cmd *cobra.Command, args []string) error {
			target := opts.ConfigPath
			if target == "" {
				target = config.DefaultConfigFile
			}
			target = filepath.Clean(target)
			if _, err := os.Stat(target); err == nil && !opts.Force {
				return exitErr(printer, fmt.Errorf("%s already exists (use --force to overwrite)", target))
			}
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return exitErr(printer, fmt.Errorf("create config directory: %w", err))
			}
			dataDir := filepath.Join(filepath.Dir(target), filepath.Dir(config.DefaultSQLitePath))
			if err := os.MkdirAll(dataDir, 0o755); err != nil {
				return exitErr(printer, fmt.Errorf("create data directory: %w", err))
			}
			if err := os.WriteFile(target, config.StarterYAML(), 0o644); err != nil {
				return exitErr(printer, fmt.Errorf("write %s: %w", target, err))
			}
			result := map[string]string{
				"config":  target,
				"dataDir": dataDir,
			}
			if printer.Format == app.OutputJSON {
				return printer.Print(result)
			}
			writeStdout(printer, fmt.Sprintf("created %s", target))
			writeStdout(printer, fmt.Sprintf("created %s", result["dataDir"]))
			return nil
		},
	}
	cmd.Flags().BoolVar(&opts.Force, "force", false, "Overwrite an existing haistack.yaml")
	return cmd
}
