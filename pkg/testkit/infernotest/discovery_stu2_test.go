package infernotest_test

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/degoke/health-ai-stack/pkg/testkit/infernotest"
)

func TestInfernoDiscoverySTU2(t *testing.T) {
	t.Parallel()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = listener.Close() }()

	baseURL := "http://" + listener.Addr().String()
	handler, meta, cleanup, err := infernotest.BuildReferenceHandler(context.Background(), baseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()

	srv := httptest.NewUnstartedServer(handler)
	srv.Listener = listener
	srv.Start()
	defer srv.Close()

	if meta.FHIRBaseURL != baseURL+"/fhir" {
		t.Fatalf("FHIR base = %q, want %q/fhir", meta.FHIRBaseURL, baseURL)
	}

	raw, headers, status, err := infernotest.FetchWellKnownConfiguration(
		context.Background(),
		http.DefaultClient,
		meta.FHIRBaseURL,
	)
	if err != nil {
		t.Fatal(err)
	}

	config := infernotest.AssertWellKnownEndpoint(t, status, headers, raw)
	infernotest.AssertWellKnownCapabilitiesSTU2(t, config)
}

func TestInfernoReferenceServerLive(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping live reference server smoke test in short mode")
	}

	_, meta, cleanup, err := infernotest.StartReferenceServer(context.Background(), "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()

	raw, headers, status, err := infernotest.FetchWellKnownConfiguration(
		context.Background(),
		http.DefaultClient,
		meta.FHIRBaseURL,
	)
	if err != nil {
		t.Fatal(err)
	}

	config := infernotest.AssertWellKnownEndpoint(t, status, headers, raw)
	infernotest.AssertWellKnownCapabilitiesSTU2(t, config)
}
