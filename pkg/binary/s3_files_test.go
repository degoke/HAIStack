package binary_test

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/degoke/health-ai-stack/pkg/binary"
	"github.com/degoke/health-ai-stack/pkg/export"
)

func TestChunkBytes(t *testing.T) {
	t.Parallel()
	if got := binary.ChunkBytes(nil, 4); len(got) != 1 || len(got[0]) != 0 {
		t.Fatalf("empty = %#v", got)
	}
	got := binary.ChunkBytes([]byte("abcdefghij"), 4)
	if len(got) != 3 || string(got[0]) != "abcd" || string(got[1]) != "efgh" || string(got[2]) != "ij" {
		t.Fatalf("chunks = %#v", got)
	}
}

func TestPrefixedFileStoreRoundTripThroughS3(t *testing.T) {
	ctx := context.Background()
	type stored struct {
		data []byte
		ct   string
	}
	var (
		mu      sync.Mutex
		objects = map[string]stored{}
	)
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := strings.TrimPrefix(r.URL.Path, "/bucket/")
		switch r.Method {
		case http.MethodPut:
			data, _ := io.ReadAll(r.Body)
			mu.Lock()
			objects[key] = stored{data: data, ct: r.Header.Get("Content-Type")}
			mu.Unlock()
			w.WriteHeader(http.StatusOK)
		case http.MethodGet:
			mu.Lock()
			obj, ok := objects[key]
			mu.Unlock()
			if !ok {
				http.NotFound(w, r)
				return
			}
			if obj.ct != "" {
				w.Header().Set("Content-Type", obj.ct)
			}
			_, _ = w.Write(obj.data)
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	})
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	inner, err := binary.NewS3BlobStore(binary.S3Config{
		Endpoint:        server.URL,
		Region:          "us-east-1",
		Bucket:          "bucket",
		AccessKeyID:     "access",
		SecretAccessKey: "secret",
		UsePathStyle:    true,
		HTTPClient:      server.Client(),
	})
	if err != nil {
		t.Fatalf("NewS3BlobStore: %v", err)
	}
	files := export.NewBlobFileStore(binary.AsStore(inner))
	body := []byte("{\"resourceType\":\"Patient\",\"id\":\"p1\"}\n")
	if err := files.Put(ctx, "job-1/Patient.ndjson", body, "application/fhir+ndjson"); err != nil {
		t.Fatalf("Put: %v", err)
	}
	got, ct, err := files.Get(ctx, "job-1/Patient.ndjson")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if ct != "application/fhir+ndjson" || !bytes.Equal(got, body) {
		t.Fatalf("got %q %q", ct, got)
	}
}
