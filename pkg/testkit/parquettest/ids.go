package parquettest

import (
	"io"
	"testing"

	"github.com/parquet-go/parquet-go"
)

// IDs reads non-empty values from the parquet "id" column (flat view or FHIR export).
func IDs(t *testing.T, data []byte) []string {
	t.Helper()
	return StringColumn(t, data, "id")
}

// StringColumn reads non-empty string values from a top-level parquet column.
func StringColumn(t *testing.T, data []byte, column string) []string {
	t.Helper()
	if column == "" {
		t.Fatal("parquettest: column name is required")
	}
	values, err := readStringColumn(data, column)
	if err != nil {
		t.Fatalf("parquettest.Read column %q: %v", column, err)
	}
	return values
}

func readStringColumn(data []byte, column string) ([]string, error) {
	file, err := parquet.OpenFile(bytesReader(data), int64(len(data)))
	if err != nil {
		return nil, err
	}
	col := file.Root().Column(column)
	if col == nil {
		return nil, errColumnNotFound{column: column}
	}
	var values []string
	pages := col.Pages()
	defer pages.Close()
	for {
		page, err := pages.ReadPage()
		if err != nil {
			if err == io.EOF {
				break
			}
			return values, err
		}
		if page == nil {
			break
		}
		reader := page.Values()
		buf := make([]parquet.Value, 128)
		for {
			n, err := reader.ReadValues(buf)
			for i := 0; i < n; i++ {
				if buf[i].IsNull() {
					continue
				}
				value := string(buf[i].ByteArray())
				if value != "" {
					values = append(values, value)
				}
			}
			if err == io.EOF {
				break
			}
			if err != nil {
				return values, err
			}
		}
	}
	return values, nil
}

type errColumnNotFound struct {
	column string
}

func (e errColumnNotFound) Error() string {
	return "parquet column not found: " + e.column
}

type bytesReader []byte

func (b bytesReader) ReadAt(p []byte, off int64) (int, error) {
	if off >= int64(len(b)) {
		return 0, io.EOF
	}
	return copy(p, b[off:]), nil
}
