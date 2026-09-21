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
// chunk. write must not retain chunk after it returns. An empty reader yields one
// empty chunk so Get can assemble a valid blob. Peak memory is O(size), not O(file).
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
			if err := write(index, buf[:n]); err != nil {
				return "", total, index, err
			}
			_, _ = h.Write(buf[:n])
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
