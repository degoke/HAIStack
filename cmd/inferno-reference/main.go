package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/degoke/health-ai-stack/pkg/testkit/infernotest"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "inferno-reference: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	addr := os.Getenv("INFERNO_REFERENCE_ADDR")
	if addr == "" {
		addr = "127.0.0.1:8080"
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	_, meta, cleanup, err := infernotest.StartReferenceServer(ctx, addr)
	if err != nil {
		return err
	}
	defer cleanup()

	fmt.Printf("inferno-reference listening on %s\n", meta.BaseURL)
	fmt.Printf("FHIR base URL: %s\n", meta.FHIRBaseURL)
	fmt.Printf("SMART discovery: %s\n", infernotest.WellKnownURL(meta.FHIRBaseURL))

	healthClient := &http.Client{Timeout: 2 * time.Second}
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			req, err := http.NewRequestWithContext(ctx, http.MethodGet, infernotest.WellKnownURL(meta.FHIRBaseURL), nil)
			if err != nil {
				fmt.Fprintf(os.Stderr, "health check request: %v\n", err)
				continue
			}
			req.Header.Set("Accept", "application/json")
			resp, err := healthClient.Do(req)
			if err != nil {
				fmt.Fprintf(os.Stderr, "health check failed: %v\n", err)
				continue
			}
			_ = resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				fmt.Fprintf(os.Stderr, "health check status = %d\n", resp.StatusCode)
			}
		}
	}
}
