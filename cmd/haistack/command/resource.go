package command

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/degoke/health-ai-stack/cmd/haistack/internal/app"
	"github.com/spf13/cobra"
)

func newReadCommand(opts *Options, printer *app.Printer) *cobra.Command {
	return &cobra.Command{
		Use:   "read <ResourceType/id>",
		Short: "Read one stored FHIR resource",
		Args:  cobra.ExactArgs(1),
		Example: `  haistack read Patient/123
  haistack read Patient/123 --output json`,
		RunE: func(cmd *cobra.Command, args []string) error {
			resourceType, id, err := app.ParseResourceReference(args[0])
			if err != nil {
				return exitErr(printer, err)
			}
			if id == "" {
				return exitErr(printer, fmt.Errorf("read requires a resource id"))
			}
			ctx := context.Background()
			session, err := openSession(opts, printer, ctx)
			if err != nil {
				return err
			}
			defer func() { _ = session.Close(ctx) }()
			resource, err := session.Runtime.Services().ResourceService.Read(ctx, resourceType, id)
			if err != nil {
				return exitErr(printer, err)
			}
			return printer.Print(json.RawMessage(resource.JSON))
		},
	}
}

func newDeleteCommand(opts *Options, printer *app.Printer) *cobra.Command {
	var force bool
	cmd := &cobra.Command{
		Use:   "delete <ResourceType/id>",
		Short: "Delete one stored FHIR resource",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if !force {
				return exitErr(printer, fmt.Errorf("deleting a resource is destructive; repeat with --force"))
			}
			resourceType, id, err := app.ParseResourceReference(args[0])
			if err != nil {
				return exitErr(printer, err)
			}
			if id == "" {
				return exitErr(printer, fmt.Errorf("delete requires a resource id"))
			}
			ctx := context.Background()
			session, err := openSession(opts, printer, ctx)
			if err != nil {
				return err
			}
			defer func() { _ = session.Close(ctx) }()
			if err := session.Runtime.Services().ResourceService.Delete(ctx, resourceType, id); err != nil {
				return exitErr(printer, err)
			}
			result := map[string]any{"resourceType": resourceType, "id": id, "status": "deleted"}
			if printer.Format == app.OutputJSON {
				return printer.Print(result)
			}
			writeStdout(printer, fmt.Sprintf("deleted %s/%s", resourceType, id))
			return nil
		},
	}
	cmd.Flags().BoolVar(&force, "force", false, "Confirm deletion of the resource")
	return cmd
}

func newExportCommand(opts *Options, printer *app.Printer) *cobra.Command {
	var limit int
	cmd := &cobra.Command{
		Use:   "export <ResourceType[/id]>",
		Short: "Export one resource or a collection of resources as JSON",
		Args:  cobra.ExactArgs(1),
		Example: `  haistack export Patient/123
  haistack export Patient --limit 100 --output json`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if limit < 0 {
				return exitErr(printer, fmt.Errorf("--limit cannot be negative"))
			}
			resourceType, id, err := app.ParseResourceReference(args[0])
			if err != nil {
				return exitErr(printer, err)
			}
			ctx := context.Background()
			session, err := openSession(opts, printer, ctx)
			if err != nil {
				return err
			}
			defer func() { _ = session.Close(ctx) }()

			if id != "" {
				resource, err := session.Runtime.Services().ResourceService.Read(ctx, resourceType, id)
				if err != nil {
					return exitErr(printer, err)
				}
				return printer.Print(json.RawMessage(resource.JSON))
			}

			resources, err := session.ExportResources(ctx, resourceType, limit)
			if err != nil {
				return exitErr(printer, err)
			}
			payload := make([]json.RawMessage, 0, len(resources))
			for _, resource := range resources {
				payload = append(payload, json.RawMessage(resource.JSON))
			}
			return printer.Print(payload)
		},
	}
	cmd.Flags().IntVar(&limit, "limit", 0, "Maximum resources to export (0 means all)")
	return cmd
}

func openSession(opts *Options, printer *app.Printer, ctx context.Context) (*app.Session, error) {
	cfg, err := opts.loadConfig()
	if err != nil {
		return nil, exitErr(printer, err)
	}
	session, err := app.OpenSession(ctx, cfg)
	if err != nil {
		return nil, exitErr(printer, err)
	}
	return session, nil
}
