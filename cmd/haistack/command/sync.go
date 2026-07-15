package command

import (
	"context"
	"fmt"

	"github.com/degoke/health-ai-stack/cmd/haistack/internal/app"
	"github.com/spf13/cobra"
)

func newSyncCommand(opts *Options, printer *app.Printer) *cobra.Command {
	root := &cobra.Command{
		Use:   "sync",
		Short: "Device synchronization commands",
	}

	root.AddCommand(
		newSyncPushCommand(opts, printer),
		newSyncPullCommand(opts, printer),
		newSyncStatusCommand(opts, printer),
	)
	return root
}

func newSyncPushCommand(opts *Options, printer *app.Printer) *cobra.Command {
	return &cobra.Command{
		Use:   "push",
		Short: "Push pending local outbox events to the configured sync hub",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSyncAction(opts, printer, "push")
		},
	}
}

func newSyncPullCommand(opts *Options, printer *app.Printer) *cobra.Command {
	return &cobra.Command{
		Use:   "pull",
		Short: "Pull canonical hub events and apply them locally",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSyncAction(opts, printer, "pull")
		},
	}
}

func newSyncStatusCommand(opts *Options, printer *app.Printer) *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show local sync status from configured stores",
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

			reader, err := app.StatusReaderForSession(session)
			if err != nil {
				return exitErr(printer, err)
			}
			report, err := app.ReadSyncStatus(ctx, cfg, reader)
			if err != nil {
				return exitErr(printer, err)
			}
			if printer.Format == app.OutputJSON {
				return printer.Print(report)
			}
			writeStdout(printer, app.FormatSyncStatusText(report))
			return nil
		},
	}
}

func runSyncAction(opts *Options, printer *app.Printer, action string) error {
	cfg, err := opts.loadConfig()
	if err != nil {
		return exitErr(printer, err)
	}
	if cfg.Sync.HubURL == "" {
		return exitErr(printer, fmt.Errorf("sync hub URL is not configured (set sync.hubURL in haistack.yaml or --sync-hub-url)"))
	}
	ctx := context.Background()
	session, err := app.OpenSession(ctx, cfg)
	if err != nil {
		return exitErr(printer, err)
	}
	defer func() { _ = session.Close(ctx) }()

	engine := session.Runtime.SyncEngine()
	if engine == nil {
		return exitErr(printer, fmt.Errorf("sync engine is not configured"))
	}
	switch action {
	case "push":
		summary, err := engine.Push(ctx)
		if err != nil {
			return exitErr(printer, err)
		}
		if printer.Format == app.OutputJSON {
			return printer.Print(summary)
		}
		writeStdout(printer, fmt.Sprintf("proposed %d events (cursor=%d)", summary.Proposed, summary.Cursor))
	case "pull":
		summary, err := engine.Pull(ctx)
		if err != nil {
			return exitErr(printer, err)
		}
		if printer.Format == app.OutputJSON {
			return printer.Print(summary)
		}
		writeStdout(printer, fmt.Sprintf("fetched %d applied %d skipped %d (cursor=%d)",
			summary.Fetched, summary.Applied, summary.Skipped, summary.Cursor))
	}
	return nil
}
