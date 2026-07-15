package command

import (
	"context"

	"github.com/degoke/health-ai-stack/cmd/haistack/internal/app"
	"github.com/spf13/cobra"
)

func newImportCommand(opts *Options, printer *app.Printer) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "import <file>",
		Short: "Import one JSON FHIR resource file (create or update)",
		Args:  cobra.ExactArgs(1),
		Example: `  haistack import patient.json
  haistack import patient.json --output json`,
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

			env, err := app.ReadResourceFile(args[0])
			if err != nil {
				return exitErr(printer, err)
			}
			svc := session.Runtime.Services().ResourceService
			action, saved, err := app.UpsertResource(ctx, svc, env)
			if err != nil {
				return exitErr(printer, err)
			}
			result := map[string]any{
				"action":       action,
				"resourceType": saved.ResourceType,
				"id":           saved.ID,
				"version":      saved.VersionID,
			}
			if printer.Format == app.OutputJSON {
				return printer.Print(result)
			}
			writeStdout(printer, action+" "+saved.ResourceType+"/"+saved.ID+" v"+saved.VersionID)
			return nil
		},
	}
	return cmd
}
