package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/degoke/haistack/pkg/testkit/infernotest"
)

func main() {
	addr := os.Getenv("INFERNO_REFERENCE_ADDR")
	if addr == "" {
		addr = "127.0.0.1:8080"
	}
	_, meta, cleanup, err := infernotest.StartReferenceServer(context.Background(), addr)
	if err != nil {
		fmt.Fprintf(os.Stderr, "inferno-reference: %v\n", err)
		os.Exit(1)
	}
	defer cleanup()

	fmt.Printf("Inferno reference host listening on %s\n", addr)
	fmt.Printf("SMART discovery: %s/.well-known/smart-configuration\n", meta.FHIRBaseURL)
	fmt.Printf("OAuth issuer: %s\n", meta.BaseURL)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	<-ctx.Done()
}
