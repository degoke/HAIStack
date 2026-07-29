package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/degoke/health-ai-stack/examples/internal/appkit"
	"github.com/degoke/health-ai-stack/examples/internal/synchub"
	hasync "github.com/degoke/health-ai-stack/pkg/sync"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "sync-two-nodes: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	ctx := context.Background()
	tempDir, err := os.MkdirTemp("", "haistack-sync-two-nodes-*")
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(tempDir) }()

	nodeA, err := appkit.NewSQLiteStack(ctx, filepath.Join(tempDir, "node-a.db"), "Patient")
	if err != nil {
		return err
	}
	defer func() { _ = nodeA.Close() }()
	nodeB, err := appkit.NewSQLiteStack(ctx, filepath.Join(tempDir, "node-b.db"), "Patient")
	if err != nil {
		return err
	}
	defer func() { _ = nodeB.Close() }()

	hub := synchub.NewMemoryHub()

	engineA := hasync.NewEngine(hasync.Config{
		NodeID:    "device-a",
		TenantID:  "tenant-demo",
		Events:    nodeA.DB.OutboxStore(),
		Cursors:   nodeA.DB.CursorStore(),
		Inbox:     nodeA.DB.InboxStore(),
		Resources: nodeA.DB.ResourceStore(),
		History:   nodeA.DB.HistoryStore(),
		Hub:       hub,
	})
	engineB := hasync.NewEngine(hasync.Config{
		NodeID:    "device-b",
		TenantID:  "tenant-demo",
		Cursors:   nodeB.DB.CursorStore(),
		Inbox:     nodeB.DB.InboxStore(),
		Resources: nodeB.DB.ResourceStore(),
		History:   nodeB.DB.HistoryStore(),
		Hub:       hub,
	})

	patient, err := appkit.EnvelopeFromJSON("Patient", appkit.PatientJSON("Emmy", "Noether", "+1-555-0120"))
	if err != nil {
		return err
	}
	created, err := nodeA.ResourceService.Create(ctx, patient)
	if err != nil {
		return err
	}

	pushSummary, err := engineA.Push(ctx)
	if err != nil {
		return err
	}
	pullSummary, err := engineB.Pull(ctx)
	if err != nil {
		return err
	}

	replicated, err := nodeB.ResourceService.Read(ctx, "Patient", created.ID)
	if err != nil {
		return err
	}

	fmt.Println("sync between two local nodes")
	fmt.Printf("push proposed=%d cursor=%d\n", pushSummary.Proposed, pushSummary.Cursor)
	fmt.Printf("pull fetched=%d applied=%d cursor=%d\n", pullSummary.Fetched, pullSummary.Applied, pullSummary.Cursor)
	fmt.Printf("replicated patient id on node B: %s\n", replicated.ID)
	fmt.Println("replicated resource:")
	fmt.Println(appkit.PrettyJSON(replicated.JSON))
	return nil
}
