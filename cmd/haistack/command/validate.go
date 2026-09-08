package command

import (
	"context"

	"github.com/degoke/health-ai-stack/cmd/haistack/internal/app"
	"github.com/degoke/health-ai-stack/pkg/validate"
	"github.com/spf13/cobra"
)

func newValidateCommand(opts *Options, printer *app.Printer) *cobra.Command {
	var full bool
	cmd := &cobra.Command{
		Use:   "validate <file>",
		Short: "Validate one JSON FHIR resource file",
		Args:  cobra.ExactArgs(1),
		Example: `  haistack validate patient.json
  haistack validate patient.json --full
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
				ProfileCatalog: validate.NewRegistryProfileCatalog(session.Runtime.Services().RegistrySnapshot),
				FHIRPath:       session.Runtime.Services().FHIRPathEngine,
			})
			if err != nil {
				return exitErr(printer, err)
			}
			valOpts := validate.ValidateOptions{
				EnforceBaseProfile:      true,
				EnforceDeclaredProfiles: true,
				ProfileConstraints:      true,
				Terminology:             session.Runtime.Services().TerminologyService,
			}
			if full {
				valOpts.Mode = validate.ValidationModeFull
			}
			result, err := engine.Validate(ctx, env, valOpts)
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
	cmd.Flags().BoolVar(&full, "full", false, "run full profile validation (slicing, SD terminology bindings, extension policy)")
	return cmd
}

var errInvalidResource = &validationError{}

type validationError struct{}

func (e *validationError) Error() string { return "resource is invalid" }
