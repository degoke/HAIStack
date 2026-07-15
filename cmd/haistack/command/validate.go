package command

import (
	"context"

	"github.com/degoke/health-ai-stack/cmd/haistack/internal/app"
	"github.com/degoke/health-ai-stack/pkg/validate"
	"github.com/spf13/cobra"
)

func newValidateCommand(opts *Options, printer *app.Printer) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "validate <file>",
		Short: "Validate one JSON FHIR resource file",
		Args:  cobra.ExactArgs(1),
		Example: `  haistack validate patient.json
  haistack validate patient.json --output json`,
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

			engine, err := validate.NewEngine(validate.Config{
				InstalledTypes: session.Runtime.Services().RegistrySnapshot,
			})
			if err != nil {
				return exitErr(printer, err)
			}
			result, err := engine.Validate(ctx, env, validate.ValidateOptions{})
			if err != nil {
				return exitErr(printer, err)
			}

			payload := map[string]any{
				"valid":  result.Valid,
				"issues": result.Issues,
			}
			if printer.Format == app.OutputJSON {
				if err := printer.Print(payload); err != nil {
					return exitErr(printer, err)
				}
			} else if result.Valid {
				writeStdout(printer, "valid")
			} else {
				for _, issue := range result.Issues {
					writeStdout(printer, issue.Severity+": "+issue.Diagnostics)
				}
			}
			if !result.Valid {
				return errInvalidResource
			}
			return nil
		},
	}
	return cmd
}

var errInvalidResource = &validationError{}

type validationError struct{}

func (e *validationError) Error() string { return "resource is invalid" }
