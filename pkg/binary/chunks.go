package binary

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
)

// ChunkBytes splits data into slices of at most size bytes.
// An empty payload yields one empty chunk so Get can assemble a valid blob.
func ChunkBytes(data []byte, size int) [][]byte {
	if size <= 0 {
		size = DefaultChunkSize
	}
	if len(data) == 0 {
		return [][]byte{[]byte{}}
	}
	chunks := make([][]byte, 0, (len(data)+size-1)/size)
	for offset := 0; offset < len(data); offset += size {
		end := offset + size
		if end > len(data) {
			end = len(data)
		}
		chunks = append(chunks, data[offset:end])
	}
	return chunks
}

func copyBytes(data []byte) []byte {
	out := make([]byte, len(data))
	copy(out, data)
	return out
}

// CopyChunks reads r in slices of at most size bytes and invokes write for each
// chunk. write may retain chunk after it returns; CopyChunks copies out of the
// reuse buffer first. An empty reader yields one empty chunk so Get can assemble
// a valid blob. Peak memory is O(size), not O(file).
func CopyChunks(r io.Reader, size int, write func(index int, chunk []byte) error) (digest string, total int64, count int, err error) {
	if r == nil {
		return "", 0, 0, fmt.Errorf("%w: reader is required", ErrInvalidArgument)
	}
	if write == nil {
		return "", 0, 0, fmt.Errorf("%w: chunk writer is required", ErrInvalidArgument)
	}
	if size <= 0 {
		size = DefaultChunkSize
	}
	h := sha256.New()
	buf := make([]byte, size)
	index := 0
	for {
		n, readErr := io.ReadFull(r, buf)
		if n > 0 {
			owned := append([]byte(nil), buf[:n]...)
			if err := write(index, owned); err != nil {
				return "", total, index, err
			}
			_, _ = h.Write(owned)
			total += int64(n)
			index++
		}
		if readErr == io.EOF || readErr == io.ErrUnexpectedEOF {
			break
		}
		if readErr != nil {
			return "", total, index, readErr
		}
	}
	if index == 0 {
		if err := write(0, []byte{}); err != nil {
			return "", 0, 0, err
		}
		index = 1
	}
	return hex.EncodeToString(h.Sum(nil)), total, index, nil
}

// ChunkReader streams a chunked blob one index at a time without assembling the
// full payload. Close is a no-op.
type ChunkReader struct {
	ctx   context.Context
	read  func(ctx context.Context, index int) ([]byte, error)
	index int
	count int
	buf   []byte
	off   int
}

// NewChunkReader returns a reader over count chunks. read is called with indexes
// in [0, count).
func NewChunkReader(ctx context.Context, count int, read func(ctx context.Context, index int) ([]byte, error)) *ChunkReader {
	if count < 0 {
		count = 0
	}
	return &ChunkReader{ctx: ctx, read: read, count: count}
}

func (r *ChunkReader) Read(p []byte) (int, error) {
	if r == nil {
		return 0, io.EOF
	}
	if len(p) == 0 {
		return 0, nil
	}
	for r.off >= len(r.buf) {
		if r.index >= r.count {
			return 0, io.EOF
		}
		if r.read == nil {
			return 0, fmt.Errorf("%w: chunk reader is required", ErrInvalidArgument)
		}
		chunk, err := r.read(r.ctx, r.index)
		r.index++
		if err != nil {
			return 0, err
		}
		r.buf = chunk
		r.off = 0
	}
	n := copy(p, r.buf[r.off:])
	r.off += n
	return n, nil
}

func (r *ChunkReader) Close() error { return nil }

// PutBlobStream writes a blob from r. When blobs implements BlobStoreWithStream,
// the payload is streamed; otherwise the helper reads the full reader into memory
// and calls Put.
func PutBlobStream(ctx context.Context, blobs BlobStore, blobID string, r io.Reader, size int64, contentType string) (*BlobDescriptor, error) {
	if blobs == nil {
		return nil, fmt.Errorf("%w: blob store is required", ErrInvalidArgument)
	}
	if r == nil {
		return nil, fmt.Errorf("%w: reader is required", ErrInvalidArgument)
	}
	if streamer, ok := blobs.(BlobStoreWithStream); ok {
		return streamer.PutStream(ctx, blobID, r, size, contentType)
	}
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}
	return blobs.Put(ctx, blobID, data, contentType)
}
