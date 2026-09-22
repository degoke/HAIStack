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

func TestCopyChunksStreamsWithoutFullFileBuffer(t *testing.T) {
	t.Parallel()
	payload := []byte("abcdefghij")
	probe := &copyChunksProbe{r: bytes.NewReader(payload)}
	var got [][]byte
	hash, total, count, err := binary.CopyChunks(probe, 4, func(index int, chunk []byte) error {
		got = append(got, append([]byte(nil), chunk...))
		return nil
	})
	if err != nil {
		t.Fatalf("CopyChunks: %v", err)
	}
	if total != 10 || count != 3 {
		t.Fatalf("total=%d count=%d", total, count)
	}
	if hash != binary.HashSHA256(payload) {
		t.Fatalf("hash=%s", hash)
	}
	if string(got[0]) != "abcd" || string(got[1]) != "efgh" || string(got[2]) != "ij" {
		t.Fatalf("chunks = %#v", got)
	}
	if probe.maxRead > 4 {
		t.Fatalf("max Read dest %d > chunk size 4", probe.maxRead)
	}
	emptyHash, emptyTotal, emptyCount, err := binary.CopyChunks(bytes.NewReader(nil), 4, func(index int, chunk []byte) error {
		if index != 0 || len(chunk) != 0 {
			t.Fatalf("empty chunk index=%d len=%d", index, len(chunk))
		}
		return nil
	})
	if err != nil || emptyTotal != 0 || emptyCount != 1 {
		t.Fatalf("empty: hash=%s total=%d count=%d err=%v", emptyHash, emptyTotal, emptyCount, err)
	}
	if emptyHash != binary.HashSHA256(nil) {
		t.Fatalf("empty hash=%s", emptyHash)
	}
}

func TestCopyChunksWriteMayRetainChunk(t *testing.T) {
	t.Parallel()
	payload := []byte("abcdefghij")
	var got [][]byte
	_, _, _, err := binary.CopyChunks(bytes.NewReader(payload), 4, func(_ int, chunk []byte) error {
		got = append(got, chunk)
		return nil
	})
	if err != nil {
		t.Fatalf("CopyChunks: %v", err)
	}
	if string(got[0]) != "abcd" || string(got[1]) != "efgh" || string(got[2]) != "ij" {
		t.Fatalf("retained chunks overwritten: %#v", got)
	}
}

func TestChunkReaderStreamsChunks(t *testing.T) {
	t.Parallel()
	chunks := [][]byte{[]byte("abcd"), []byte("efgh"), []byte("ij")}
	r := binary.NewChunkReader(context.Background(), len(chunks), func(_ context.Context, index int) ([]byte, error) {
		return chunks[index], nil
	})
	got, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if string(got) != "abcdefghij" {
		t.Fatalf("got %q", got)
	}
	empty, err := io.ReadAll(binary.NewChunkReader(context.Background(), 1, func(context.Context, int) ([]byte, error) {
		return []byte{}, nil
	}))
	if err != nil || len(empty) != 0 {
		t.Fatalf("empty: %q %v", empty, err)
	}
}

type copyChunksProbe struct {
	r       io.Reader
	maxRead int
}

func (p *copyChunksProbe) Read(b []byte) (int, error) {
	if len(b) > p.maxRead {
		p.maxRead = len(b)
	}
	return p.r.Read(b)
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
	rc, openCT, err := files.Open(ctx, "job-1/Patient.ndjson")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = rc.Close() })
	openData, err := io.ReadAll(rc)
	if err != nil {
		t.Fatalf("Open read: %v", err)
	}
	if openCT != "application/fhir+ndjson" || !bytes.Equal(openData, body) {
		t.Fatalf("open %q %q", openCT, openData)
	}
}

func TestS3PutStreamRejectsSizeMismatch(t *testing.T) {
	t.Parallel()
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	client := server.Client()
	base := client.Transport
	client.Transport = roundTripDrainBody{base: base}
	store, err := binary.NewS3BlobStore(binary.S3Config{
		Endpoint:        server.URL,
		Region:          "us-east-1",
		Bucket:          "bucket",
		AccessKeyID:     "access",
		SecretAccessKey: "secret",
		UsePathStyle:    true,
		HTTPClient:      client,
	})
	if err != nil {
		t.Fatalf("NewS3BlobStore: %v", err)
	}
	_, err = store.PutStream(context.Background(), "blob-mismatch", bytes.NewReader([]byte("hello-world")), 4, "application/octet-stream")
	if err == nil {
		t.Fatal("expected size mismatch error")
	}
	if !strings.Contains(err.Error(), "declared size") {
		t.Fatalf("err = %v", err)
	}
}

type roundTripDrainBody struct {
	base http.RoundTripper
}

func (t roundTripDrainBody) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.Body != nil {
		_, _ = io.Copy(io.Discard, req.Body)
		_ = req.Body.Close()
		req.Body = http.NoBody
		req.ContentLength = 0
		req.Header.Del("Content-Length")
	}
	return t.base.RoundTrip(req)
}
