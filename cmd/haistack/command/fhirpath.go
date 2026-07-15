package command

import (
	"context"
	"fmt"

	"github.com/degoke/health-ai-stack/cmd/haistack/internal/app"
	"github.com/spf13/cobra"
)

func newFHIRPathCommand(opts *Options, printer *app.Printer) *cobra.Command {
	evalCmd := &cobra.Command{
		Use:   "eval <file> <expression>",
		Short: "Evaluate a FHIRPath expression against a JSON resource file",
		Args:  cobra.ExactArgs(2),
		Example: `  haistack fhirpath eval patient.json "Patient.name.family"
  haistack fhirpath eval patient.json "Patient.active" --output json`,
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
			engine := session.Runtime.Services().FHIRPathEngine
			if engine == nil {
				return exitErr(printer, fmt.Errorf("fhirpath engine is not configured"))
			}
			values, err := engine.Eval(ctx, args[1], env)
			if err != nil {
				return exitErr(printer, err)
			}
			if printer.Format == app.OutputJSON {
				data, err := app.FHIRPathValuesJSON(values)
				if err != nil {
					return exitErr(printer, err)
				}
				_, err = fmt.Fprintln(printer.Out, string(data))
				return err
			}
			writeStdout(printer, app.FormatFHIRPathText(values))
			return nil
		},
	}

	root := &cobra.Command{
		Use:   "fhirpath",
		Short: "FHIRPath expression tools",
	}
	root.AddCommand(evalCmd)
	return root
}
