package command

import (
	"context"
	"fmt"

	"github.com/degoke/health-ai-stack/cmd/haistack/internal/app"
	"github.com/spf13/cobra"
)

func newImportCommand(opts *Options, printer *app.Printer) *cobra.Command {
	var createOnly, updateOnly bool
	cmd := &cobra.Command{
		Use:   "import <file>",
		Short: "Import one JSON FHIR resource file",
		Args:  cobra.ExactArgs(1),
		Example: `  haistack import patient.json
	  haistack import patient.json --create-only
	  haistack import patient.json --update-only`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if createOnly && updateOnly {
				return exitErr(printer, fmt.Errorf("--create-only and --update-only cannot be used together"))
			}
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
			action, saved, err := app.ImportResource(ctx, svc, env, createOnly, updateOnly)
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
	cmd.Flags().BoolVar(&createOnly, "create-only", false, "Fail if the resource already exists")
	cmd.Flags().BoolVar(&updateOnly, "update-only", false, "Fail if the resource does not already exist")
	return cmd
}
