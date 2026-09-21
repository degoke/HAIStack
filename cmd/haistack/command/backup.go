package command

import (
	"context"
	"fmt"

	"github.com/degoke/health-ai-stack/cmd/haistack/internal/app"
	"github.com/spf13/cobra"
)

func newBackupCommand(opts *Options, printer *app.Printer) *cobra.Command {
	return &cobra.Command{
		Use:   "backup [dir]",
		Short: "Write an NDJSON backup of stored FHIR resources",
		Args:  cobra.MaximumNArgs(1),
		Example: `  haistack backup
  haistack backup ./snapshots/today --output json`,
		RunE: func(cmd *cobra.Command, args []string) error {
			dir := "backup"
			if len(args) == 1 {
				dir = args[0]
			}
			ctx := context.Background()
			session, err := openSession(opts, printer, ctx)
			if err != nil {
				return err
			}
			defer func() { _ = session.Close(ctx) }()
			report, err := session.Backup(ctx, dir)
			if err != nil {
				return exitErr(printer, err)
			}
			if printer.Format == app.OutputJSON {
				return printer.Print(report)
			}
			writeStdout(printer, fmt.Sprintf("backed up %d resource type(s) to %s", len(report.Files), report.Directory))
			for _, file := range report.Files {
				writeStdout(printer, fmt.Sprintf("  %s %d", file.File, file.Count))
			}
			return nil
		},
	}
}

func newRestoreCommand(opts *Options, printer *app.Printer) *cobra.Command {
	return &cobra.Command{
		Use:   "restore [dir]",
		Short: "Restore FHIR resources from an NDJSON backup directory",
		Args:  cobra.MaximumNArgs(1),
		Example: `  haistack restore
  haistack restore ./snapshots/today`,
		RunE: func(cmd *cobra.Command, args []string) error {
			dir := "backup"
			if len(args) == 1 {
				dir = args[0]
			}
			ctx := context.Background()
			session, err := openSession(opts, printer, ctx)
			if err != nil {
				return err
			}
			defer func() { _ = session.Close(ctx) }()
			report, err := session.Restore(ctx, dir)
			if err != nil {
				return exitErr(printer, err)
			}
			if printer.Format == app.OutputJSON {
				return printer.Print(report)
			}
			writeStdout(printer, fmt.Sprintf("restored %s: created %d, updated %d", report.Directory, report.Created, report.Updated))
			return nil
		},
	}
}
