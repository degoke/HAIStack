package command

import (
	"context"

	"github.com/degoke/health-ai-stack/cmd/haistack/internal/app"
	"github.com/spf13/cobra"
)

func newModuleCommand(opts *Options, printer *app.Printer) *cobra.Command {
	installCmd := &cobra.Command{
		Use:   "install <path>",
		Short: "Install a module from a local directory",
		Args:  cobra.ExactArgs(1),
		Example: `  haistack module install modules/core`,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := opts.loadConfig()
			if err != nil {
				return exitErr(printer, err)
			}
			ctx := context.Background()
			session, err := app.OpenSession(ctx, cfg)
			if err != nil {
				return exitErr(printer, err)
			}
			defer func() { _ = session.Close(ctx) }()

			mgr := session.Runtime.Services().ModuleManager
			result, err := mgr.Install(ctx, args[0])
			if err != nil {
				return exitErr(printer, err)
			}
			payload := map[string]any{
				"name":                 result.Name,
				"version":              result.Version,
				"enabledResources":     result.EnabledResources,
				"installedDefinitions": result.InstalledDefinitions,
				"deferred":             result.Deferred,
			}
			if printer.Format == app.OutputJSON {
				return printer.Print(payload)
			}
			writeStdout(printer, "installed "+result.Name+" "+result.Version)
			if len(result.EnabledResources) > 0 {
				writeStdout(printer, "enabled resources: "+joinComma(result.EnabledResources))
			}
			return nil
		},
	}

	root := &cobra.Command{
		Use:   "module",
		Short: "Module installation commands",
	}
	root.AddCommand(installCmd)
	return root
}

func joinComma(items []string) string {
	if len(items) == 0 {
		return ""
	}
	out := items[0]
	for i := 1; i < len(items); i++ {
		out += ", " + items[i]
	}
	return out
}
