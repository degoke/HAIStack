package infernotest_test

import (
	"context"
	"net"
	"net/http/httptest"
	"testing"

	"github.com/degoke/health-ai-stack/pkg/testkit/infernotest"
)

func TestInfernoStandaloneLaunchSTU2(t *testing.T) {
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

	infernotest.AssertStandaloneLaunchFlow(
		t,
		meta.BaseURL,
		meta.FHIRBaseURL,
		infernotest.DefaultClientID,
		infernotest.DefaultRedirectURI,
		"patient/Patient.read launch/patient openid",
	)
}
