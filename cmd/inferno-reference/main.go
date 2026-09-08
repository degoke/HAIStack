// inferno-reference is a minimal SMART-on-FHIR reference host for Inferno
// discovery CI. It serves HAIStack FHIR APIs plus .well-known/smart-configuration
// and stub OAuth authorize/token endpoints.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/degoke/health-ai-stack/pkg/smart/host"
)

func main() {
	addr := flag.String("addr", ":8765", "listen address")
	dbPath := flag.String("db", "", "sqlite database path (temp file when empty)")
	flag.Parse()

	ctx := context.Background()
	if *dbPath == "" {
		dir, err := os.MkdirTemp("", "haistack-inferno-ref-*")
		if err != nil {
			log.Fatalf("temp db dir: %v", err)
		}
		*dbPath = host.DefaultDBPath(dir)
	}

	publicBase := "http://localhost" + *addr
	stack, err := host.OpenReferenceStack(ctx, *dbPath, publicBase)
	if err != nil {
		log.Fatalf("stack: %v", err)
	}
	defer func() { _ = stack.Close() }()

	srv := &http.Server{Addr: *addr, Handler: stack.Handler}
	go func() {
		log.Printf("inferno-reference listening on %s (FHIR %s/fhir)", *addr, publicBase)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("listen: %v", err)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = srv.Shutdown(shutdownCtx)
	fmt.Println("inferno-reference stopped")
}
