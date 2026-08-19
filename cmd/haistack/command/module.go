package command

import (
	"context"
	"fmt"

	"github.com/degoke/health-ai-stack/cmd/haistack/internal/app"
	"github.com/degoke/health-ai-stack/pkg/modules"
	"github.com/spf13/cobra"
)

func newModuleCommand(opts *Options, printer *app.Printer) *cobra.Command {
	var upgradeOnly bool
	installCmd := &cobra.Command{
		Use:   "install <path>",
		Short: "Install a module from a local directory",
		Args:  cobra.ExactArgs(1),
		Example: `  haistack module install modules/core
  haistack module install modules/core --upgrade-only`,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := context.Background()
			session, err := openModuleSession(opts, printer, ctx)
			if err != nil {
				return err
			}
			defer func() { _ = session.Close(ctx) }()

			mgr := session.Runtime.Services().ModuleManager
			plan, err := mgr.PlanInstall(ctx, args[0])
			if err != nil {
				return exitErr(printer, err)
			}
			if upgradeOnly && plan.Action != "upgrade" {
				return exitErr(printer, fmt.Errorf("module %q is not already installed; --upgrade-only requires an existing module", plan.Name))
			}
			result, err := mgr.Install(ctx, args[0])
			if err != nil {
				return exitErr(printer, err)
			}
			return printInstallResult(printer, result)
		},
	}
	installCmd.Flags().BoolVar(&upgradeOnly, "upgrade-only", false, "Fail unless the module is already installed and will be upgraded")

	upgradeCmd := &cobra.Command{
		Use:   "upgrade <path>",
		Short: "Upgrade an installed module from a local directory",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := context.Background()
			session, err := openModuleSession(opts, printer, ctx)
			if err != nil {
				return err
			}
			defer func() { _ = session.Close(ctx) }()
			result, err := session.Runtime.Services().ModuleManager.Upgrade(ctx, args[0])
			if err != nil {
				return exitErr(printer, err)
			}
			payload := map[string]any{
				"action":               "upgrade",
				"name":                 result.Name,
				"oldVersion":           result.OldVersion,
				"newVersion":           result.NewVersion,
				"enabledResources":     result.EnabledResources,
				"installedDefinitions": result.InstalledDefinitions,
				"removedDefinitions":   result.RemovedDefinitions,
				"deferred":             result.Deferred,
			}
			if printer.Format == app.OutputJSON {
				return printer.Print(payload)
			}
			writeStdout(printer, fmt.Sprintf("upgraded %s %s -> %s", result.Name, result.OldVersion, result.NewVersion))
			if len(result.EnabledResources) > 0 {
				writeStdout(printer, "enabled resources: "+joinComma(result.EnabledResources))
			}
			return nil
		},
	}

	planCmd := &cobra.Command{
		Use:   "plan <path>",
		Short: "Show the changes a module install or upgrade would make",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := context.Background()
			session, err := openModuleSession(opts, printer, ctx)
			if err != nil {
				return err
			}
			defer func() { _ = session.Close(ctx) }()
			plan, err := session.Runtime.Services().ModuleManager.PlanInstall(ctx, args[0])
			if err != nil {
				return exitErr(printer, err)
			}
			if printer.Format == app.OutputJSON {
				return printer.Print(plan)
			}
			writeStdout(printer, fmt.Sprintf("%s %s (%s)", plan.Name, plan.Version, plan.Action))
			if len(plan.ResourcesToEnable) > 0 {
				writeStdout(printer, "resources: "+joinComma(plan.ResourcesToEnable))
			}
			return nil
		},
	}

	listCmd := &cobra.Command{
		Use:   "list",
		Short: "List installed modules",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := context.Background()
			session, err := openModuleSession(opts, printer, ctx)
			if err != nil {
				return err
			}
			defer func() { _ = session.Close(ctx) }()
			items, err := session.Runtime.Services().ModuleManager.List(ctx)
			if err != nil {
				return exitErr(printer, err)
			}
			if printer.Format == app.OutputJSON {
				return printer.Print(items)
			}
			if len(items) == 0 {
				writeStdout(printer, "no installed modules")
				return nil
			}
			for _, item := range items {
				writeStdout(printer, item.Name+" "+item.Version)
			}
			return nil
		},
	}

	inspectCmd := &cobra.Command{
		Use:   "inspect <name>",
		Short: "Inspect one installed module",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := context.Background()
			session, err := openModuleSession(opts, printer, ctx)
			if err != nil {
				return err
			}
			defer func() { _ = session.Close(ctx) }()
			item, err := session.Runtime.Services().ModuleManager.Inspect(ctx, args[0])
			if err != nil {
				return exitErr(printer, err)
			}
			if printer.Format == app.OutputJSON {
				return printer.Print(item)
			}
			writeStdout(printer, item.Name+" "+item.Version)
			if len(item.Resources) > 0 {
				writeStdout(printer, "resources: "+joinComma(item.Resources))
			}
			return nil
		},
	}

	var force bool
	uninstallCmd := &cobra.Command{
		Use:   "uninstall <name>",
		Short: "Uninstall an installed module",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if !force {
				return exitErr(printer, fmt.Errorf("uninstalling a module is destructive; repeat with --force"))
			}
			ctx := context.Background()
			session, err := openModuleSession(opts, printer, ctx)
			if err != nil {
				return err
			}
			defer func() { _ = session.Close(ctx) }()
			if err := session.Runtime.Services().ModuleManager.Uninstall(ctx, args[0]); err != nil {
				return exitErr(printer, err)
			}
			if printer.Format == app.OutputJSON {
				return printer.Print(map[string]any{"name": args[0], "status": "uninstalled"})
			}
			writeStdout(printer, "uninstalled "+args[0])
			return nil
		},
	}
	uninstallCmd.Flags().BoolVar(&force, "force", false, "Confirm removal of the module and its registry contributions")

	root := &cobra.Command{
		Use:   "module",
		Short: "Module lifecycle commands",
	}
	root.AddCommand(installCmd, upgradeCmd, planCmd, listCmd, inspectCmd, uninstallCmd)
	return root
}

func openModuleSession(opts *Options, printer *app.Printer, ctx context.Context) (*app.Session, error) {
	cfg, err := opts.loadConfig()
	if err != nil {
		return nil, exitErr(printer, err)
	}
	// Module lifecycle commands operate on the persisted registry explicitly.
	// Do not auto-install config module paths first, otherwise `module upgrade`
	// can upgrade during session construction and have nothing left to do.
	cfg.Runtime.ModulePaths = nil
	session, err := app.OpenSession(ctx, cfg)
	if err != nil {
		return nil, exitErr(printer, err)
	}
	return session, nil
}

func printInstallResult(printer *app.Printer, result modules.InstallResult) error {
	payload := map[string]any{
		"action":               result.Action,
		"name":                 result.Name,
		"version":              result.Version,
		"enabledResources":     result.EnabledResources,
		"installedDefinitions": result.InstalledDefinitions,
		"deferred":             result.Deferred,
	}
	if printer.Format == app.OutputJSON {
		return printer.Print(payload)
	}
	if result.Action == "noop" {
		writeStdout(printer, "already installed "+result.Name+" "+result.Version)
	} else {
		writeStdout(printer, result.Action+"ed "+result.Name+" "+result.Version)
	}
	if len(result.EnabledResources) > 0 {
		writeStdout(printer, "enabled resources: "+joinComma(result.EnabledResources))
	}
	return nil
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
