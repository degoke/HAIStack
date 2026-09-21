package parquetfhir

import (
	"encoding/binary"
	"fmt"
	"io"
	"time"

	"github.com/parquet-go/parquet-go"
	"github.com/parquet-go/parquet-go/compress"
	"github.com/parquet-go/parquet-go/deprecated"
	"github.com/parquet-go/parquet-go/encoding/thrift"
	"github.com/parquet-go/parquet-go/format"
)

// ReadInt96MillisColumn reads named INT96 TIMESTAMP(MILLIS) annotation values.
//
// parquet-go's OpenFile remaps TIMESTAMP logical types to INT64 regardless of
// the footer's physical type, so Schema().Lookup and Pages() fail with
// "cannot decode INT64 from input of size 12". This helper uses file metadata
// physical type INT96 and decodes PLAIN pages as 12-byte millis-packed values.
func ReadInt96MillisColumn(r io.ReaderAt, size int64, name string) ([]time.Time, error) {
	file, err := parquet.OpenFile(r, size)
	if err != nil {
		return nil, fmt.Errorf("parquetfhir: open INT96 file: %w", err)
	}
	meta := file.Metadata()
	colIndex, elem, err := leafColumnByName(meta.Schema, name)
	if err != nil {
		return nil, err
	}
	if !elem.Type.Valid || elem.Type.V != format.Int96 {
		return nil, fmt.Errorf("parquetfhir: column %q physical type is not INT96", name)
	}

	var out []time.Time
	for _, rg := range meta.RowGroups {
		if colIndex >= len(rg.Columns) {
			return nil, fmt.Errorf("parquetfhir: row group missing column %q", name)
		}
		values, err := decodeInt96Chunk(r, rg.Columns[colIndex])
		if err != nil {
			return nil, fmt.Errorf("parquetfhir: decode INT96 column %q: %w", name, err)
		}
		for _, v := range values {
			out = append(out, int96MillisToTime(v))
		}
	}
	return out, nil
}

func int96MillisToTime(v deprecated.Int96) time.Time {
	return time.UnixMilli(v.Int64()).UTC()
}

func leafColumnByName(schema []format.SchemaElement, name string) (int, format.SchemaElement, error) {
	leaf := 0
	for i := 1; i < len(schema); i++ {
		elem := schema[i]
		if !elem.Type.Valid {
			continue
		}
		if elem.Name == name {
			return leaf, elem, nil
		}
		leaf++
	}
	return 0, format.SchemaElement{}, fmt.Errorf("parquetfhir: column %q not found", name)
}

func decodeInt96Chunk(r io.ReaderAt, chunk format.ColumnChunk) ([]deprecated.Int96, error) {
	offset := chunk.MetaData.DataPageOffset
	if chunk.MetaData.DictionaryPageOffset != 0 {
		offset = chunk.MetaData.DictionaryPageOffset
	}
	size := chunk.MetaData.TotalCompressedSize
	if size <= 0 {
		return nil, nil
	}
	buf := make([]byte, size)
	if _, err := r.ReadAt(buf, offset); err != nil && err != io.EOF {
		return nil, err
	}

	codec := parquet.LookupCompressionCodec(chunk.MetaData.Codec)
	reader := newBytesCursor(buf)
	decoder := thrift.NewDecoder((&thrift.CompactProtocol{}).NewReader(reader))
	var values []deprecated.Int96
	for reader.Len() > 0 {
		var header format.PageHeader
		if err := decoder.Decode(&header); err != nil {
			if err == io.EOF {
				break
			}
			return nil, err
		}
		page, err := reader.ReadN(int(header.CompressedPageSize))
		if err != nil {
			return nil, err
		}
		switch header.Type {
		case format.DictionaryPage:
			continue
		case format.DataPageV2:
			pageValues, err := decodeInt96DataPageV2(header, page, codec)
			if err != nil {
				return nil, err
			}
			values = append(values, pageValues...)
		case format.DataPage:
			pageValues, err := decodeInt96DataPageV1(header, page, codec)
			if err != nil {
				return nil, err
			}
			values = append(values, pageValues...)
		default:
			return nil, fmt.Errorf("unsupported page type %s", header.Type)
		}
	}
	return values, nil
}

func decodeInt96DataPageV2(header format.PageHeader, page []byte, codec compress.Codec) ([]deprecated.Int96, error) {
	if !header.DataPageHeaderV2.Valid {
		return nil, fmt.Errorf("missing DataPageHeaderV2")
	}
	v2 := header.DataPageHeaderV2.V
	skip := int(v2.RepetitionLevelsByteLength) + int(v2.DefinitionLevelsByteLength)
	if skip > len(page) {
		return nil, fmt.Errorf("INT96 v2 page shorter than level prefix")
	}
	data := page[skip:]
	compressed := true
	if v2.IsCompressed.Valid {
		compressed = v2.IsCompressed.V
	}
	if compressed && codec != nil {
		decoded, err := codec.Decode(nil, data)
		if err != nil {
			return nil, err
		}
		data = decoded
	}
	return decodePlainInt96(data)
}

func decodeInt96DataPageV1(header format.PageHeader, page []byte, codec compress.Codec) ([]deprecated.Int96, error) {
	data := page
	if codec != nil {
		decoded, err := codec.Decode(nil, data)
		if err != nil {
			return nil, err
		}
		data = decoded
	}
	if !header.DataPageHeader.Valid {
		return nil, fmt.Errorf("missing DataPageHeader")
	}
	// Optional INT96 annotations are written as DataPage V2. V1 pages from this
	// encoder have no repetition/definition level prefix when the column is required;
	// remaining bytes are PLAIN INT96.
	return decodePlainInt96(data)
}

func decodePlainInt96(data []byte) ([]deprecated.Int96, error) {
	if len(data) == 0 {
		return nil, nil
	}
	if len(data)%12 != 0 {
		return nil, fmt.Errorf("INT96 PLAIN page length %d is not a multiple of 12", len(data))
	}
	out := make([]deprecated.Int96, len(data)/12)
	for i := range out {
		off := i * 12
		out[i][0] = binary.LittleEndian.Uint32(data[off : off+4])
		out[i][1] = binary.LittleEndian.Uint32(data[off+4 : off+8])
		out[i][2] = binary.LittleEndian.Uint32(data[off+8 : off+12])
	}
	return out, nil
}

type bytesCursor struct {
	b []byte
	i int
}

func newBytesCursor(b []byte) *bytesCursor {
	return &bytesCursor{b: b}
}

func (c *bytesCursor) Read(p []byte) (int, error) {
	if c.i >= len(c.b) {
		return 0, io.EOF
	}
	n := copy(p, c.b[c.i:])
	c.i += n
	return n, nil
}

func (c *bytesCursor) ReadN(n int) ([]byte, error) {
	if n < 0 {
		return nil, fmt.Errorf("negative page size")
	}
	if c.i+n > len(c.b) {
		return nil, io.ErrUnexpectedEOF
	}
	out := c.b[c.i : c.i+n]
	c.i += n
	return out, nil
}

func (c *bytesCursor) Len() int {
	return len(c.b) - c.i
}
