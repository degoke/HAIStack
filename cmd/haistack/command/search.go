package command

import (
	"context"
	"fmt"
	"net/url"

	"github.com/degoke/health-ai-stack/cmd/haistack/internal/app"
	"github.com/spf13/cobra"
)

func newSearchCommand(opts *Options, printer *app.Printer) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "search <ResourceType> [key=value ...]",
		Short: "Search resources using FHIR search parameters",
		Args:  cobra.MinimumNArgs(1),
		Example: `  haistack search Patient name=Smith
  haistack search Patient family=Doe given=Jane`,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := opts.loadConfig()
			if err != nil {
				return exitErr(printer, err)
			}
			if !cfg.Runtime.EnableSearch {
				return exitErr(printer, fmt.Errorf("search is not enabled in configuration"))
			}
			ctx := context.Background()
			session, err := app.OpenSession(ctx, cfg)
			if err != nil {
				return exitErr(printer, err)
			}
			defer func() { _ = session.Close(ctx) }()

			searchSvc := session.Runtime.Services().SearchService
			if searchSvc == nil {
				return exitErr(printer, fmt.Errorf("search service is not configured"))
			}

			resourceType := args[0]
			paramsMap, err := app.ParseSearchParams(args[1:])
			if err != nil {
				return exitErr(printer, err)
			}
			values := url.Values{}
			for key, vals := range paramsMap {
				for _, v := range vals {
					values.Add(key, v)
				}
			}

			bundle, err := searchSvc.SearchBundle(ctx, resourceType, values)
			if err != nil {
				return exitErr(printer, err)
			}
			if printer.Format == app.OutputJSON {
				return printer.Print(bundle)
			}
			if len(bundle.Entries) == 0 {
				writeStdout(printer, "no matches")
				return nil
			}
			for _, entry := range bundle.Entries {
				if entry.Resource == nil {
					continue
				}
				writeStdout(printer, fmt.Sprintf("%s/%s v%s",
					entry.Resource.ResourceType, entry.Resource.ID, entry.Resource.VersionID))
			}
			return nil
		},
	}
	return cmd
}
