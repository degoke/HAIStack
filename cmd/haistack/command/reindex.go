package command

import (
	"context"
	"fmt"

	"github.com/degoke/health-ai-stack/cmd/haistack/internal/app"
	"github.com/spf13/cobra"
)

func newReindexCommand(opts *Options, printer *app.Printer) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "reindex [ResourceType]",
		Short: "Rebuild search indexes synchronously",
		Long: `Rebuild search index rows for one resource type or all enabled types when
ResourceType is omitted. Requires search to be enabled in configuration.`,
		Example: `  haistack reindex
  haistack reindex Patient`,
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

			worker, err := session.NewReindexWorker()
			if err != nil {
				return exitErr(printer, err)
			}
			resourceType := ""
			if len(args) > 0 {
				resourceType = args[0]
			}
			if err := worker.ReindexAll(ctx, resourceType); err != nil {
				return exitErr(printer, err)
			}
			result := map[string]any{
				"resourceType": resourceType,
				"status":       "completed",
			}
			if printer.Format == app.OutputJSON {
				return printer.Print(result)
			}
			if resourceType == "" {
				writeStdout(printer, "reindexed all enabled resource types")
			} else {
				writeStdout(printer, fmt.Sprintf("reindexed %s", resourceType))
			}
			return nil
		},
	}
	return cmd
}
