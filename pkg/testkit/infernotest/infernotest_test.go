package infernotest_test

import (
	"context"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/degoke/health-ai-stack/pkg/smart/host"
	"github.com/degoke/health-ai-stack/pkg/testkit/infernotest"
)

func TestInfernoDiscoverySTU2_ReferenceHost(t *testing.T) {
	ctx := context.Background()
	base := startReferenceHost(t, ctx)
	client := &http.Client{Timeout: 5 * time.Second}

	var result infernotest.DiscoveryResult
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		result = infernotest.AssertDiscoverySTU2(client, base+"/fhir")
		if result.StatusCode == http.StatusOK {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if err := result.Errorf(); err != nil {
		t.Fatal(err)
	}
}

func startReferenceHost(t *testing.T, ctx context.Context) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	publicBase := "http://127.0.0.1:" + itoa(port)

	stack, err := host.OpenReferenceStack(ctx, host.DefaultDBPath(t.TempDir()), publicBase)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = stack.Close() })

	srv := &http.Server{Handler: stack.Handler}
	go func() { _ = srv.Serve(ln) }()
	t.Cleanup(func() { _ = srv.Close() })
	return publicBase
}

func itoa(v int) string {
	if v == 0 {
		return "0"
	}
	var buf [16]byte
	i := len(buf)
	for v > 0 {
		i--
		buf[i] = byte('0' + v%10)
		v /= 10
	}
	return string(buf[i:])
}
