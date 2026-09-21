package binary

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
