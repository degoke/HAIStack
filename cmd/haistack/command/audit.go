package command

import (
	"context"
	"fmt"
	"time"

	"github.com/degoke/health-ai-stack/cmd/haistack/internal/app"
	"github.com/degoke/health-ai-stack/pkg/store"
	"github.com/spf13/cobra"
)

func newAuditCommand(opts *Options, printer *app.Printer) *cobra.Command {
	var query store.AuditQuery
	var after, before string

	listCmd := &cobra.Command{
		Use:   "list",
		Short: "List audit records",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			var err error
			query.After, err = parseAuditTime("--after", after)
			if err != nil {
				return exitErr(printer, err)
			}
			query.Before, err = parseAuditTime("--before", before)
			if err != nil {
				return exitErr(printer, err)
			}
			if query.Limit < 0 {
				return exitErr(printer, fmt.Errorf("--limit cannot be negative"))
			}
			ctx := context.Background()
			session, err := openSession(opts, printer, ctx)
			if err != nil {
				return err
			}
			defer func() { _ = session.Close(ctx) }()
			auditStore, err := session.AuditStore()
			if err != nil {
				return exitErr(printer, err)
			}
			records, err := auditStore.List(ctx, query)
			if err != nil {
				return exitErr(printer, err)
			}
			if records == nil {
				records = []store.AuditRecord{}
			}
			if printer.Format == app.OutputJSON {
				return printer.Print(records)
			}
			if len(records) == 0 {
				writeStdout(printer, "no audit records")
				return nil
			}
			for _, record := range records {
				resource := "-"
				if record.ResourceType != "" {
					resource = record.ResourceType + "/" + record.ResourceID
				}
				writeStdout(printer, fmt.Sprintf("%s %s %s %s %s", record.Timestamp.Format(time.RFC3339), record.Actor, record.Action, resource, record.Outcome))
			}
			return nil
		},
	}
	listCmd.Flags().StringVar(&query.ResourceType, "resource-type", "", "Filter by resource type")
	listCmd.Flags().StringVar(&query.ResourceID, "resource-id", "", "Filter by resource ID")
	listCmd.Flags().StringVar(&query.Actor, "actor", "", "Filter by actor")
	listCmd.Flags().StringVar(&query.Action, "action", "", "Filter by action")
	listCmd.Flags().StringVar(&query.Outcome, "outcome", "", "Filter by outcome")
	listCmd.Flags().StringVar(&query.Tenant, "tenant", "", "Filter by tenant")
	listCmd.Flags().StringVar(&query.ViewName, "view", "", "Filter by view name")
	listCmd.Flags().StringVar(&query.ToolName, "tool", "", "Filter by tool name")
	listCmd.Flags().StringVar(&query.ConversationID, "conversation", "", "Filter by conversation ID")
	listCmd.Flags().StringVar(&after, "after", "", "Only records after RFC3339 timestamp")
	listCmd.Flags().StringVar(&before, "before", "", "Only records before RFC3339 timestamp")
	listCmd.Flags().IntVar(&query.Limit, "limit", 100, "Maximum records to return")

	root := &cobra.Command{
		Use:   "audit",
		Short: "Audit log inspection commands",
	}
	root.AddCommand(listCmd)
	return root
}

func parseAuditTime(flag, value string) (time.Time, error) {
	if value == "" {
		return time.Time{}, nil
	}
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return time.Time{}, fmt.Errorf("%s must be an RFC3339 timestamp: %w", flag, err)
	}
	return parsed, nil
}
