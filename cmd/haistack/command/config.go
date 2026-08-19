package command

import (
	"net/url"
	"strings"

	"github.com/degoke/health-ai-stack/cmd/haistack/internal/app"
	"github.com/spf13/cobra"
)

func newConfigCommand(opts *Options, printer *app.Printer) *cobra.Command {
	var showSecrets bool

	showCmd := &cobra.Command{
		Use:   "show",
		Short: "Show the resolved configuration",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := opts.loadConfig()
			if err != nil {
				return exitErr(printer, err)
			}
			if !showSecrets {
				cfg.Storage.PostgresDSN = redactDSN(cfg.Storage.PostgresDSN)
			}
			return printer.Print(cfg)
		},
	}
	showCmd.Flags().BoolVar(&showSecrets, "show-secrets", false, "Include credentials in connection strings")

	validateCmd := &cobra.Command{
		Use:   "validate",
		Short: "Validate the resolved configuration without opening storage",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if _, err := opts.loadConfig(); err != nil {
				return exitErr(printer, err)
			}
			if printer.Format == app.OutputJSON {
				return printer.Print(map[string]any{"valid": true})
			}
			writeStdout(printer, "valid")
			return nil
		},
	}

	root := &cobra.Command{
		Use:   "config",
		Short: "Configuration inspection commands",
	}
	root.AddCommand(showCmd, validateCmd)
	return root
}

func redactDSN(value string) string {
	if value == "" {
		return ""
	}
	parsed, err := url.Parse(value)
	if err == nil && parsed.User != nil {
		parsed.User = url.User(parsed.User.Username())
		return parsed.String()
	}
	if strings.Contains(value, "@") {
		return "[REDACTED]"
	}
	return value
}
