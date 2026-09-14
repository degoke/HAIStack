package parquettest

import (
	"io"
	"testing"

	"github.com/parquet-go/parquet-go"
)

// IDs reads non-empty values from the parquet "id" column (flat view or FHIR export).
func IDs(t *testing.T, data []byte) []string {
	t.Helper()
	type idRow struct {
		ID string `parquet:"id"`
	}
	rows, err := parquet.Read[idRow](bytesReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("parquettest.Read IDs: %v", err)
	}
	ids := make([]string, 0, len(rows))
	for _, row := range rows {
		if row.ID != "" {
			ids = append(ids, row.ID)
		}
	}
	return ids
}

type bytesReader []byte

func (b bytesReader) ReadAt(p []byte, off int64) (int, error) {
	if off >= int64(len(b)) {
		return 0, io.EOF
	}
	return copy(p, b[off:]), nil
}
