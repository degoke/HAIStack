package parquetfhir

import (
	"encoding/binary"
	"fmt"
	"io"
	"math/bits"
	"strings"
	"time"

	"github.com/parquet-go/parquet-go"
	"github.com/parquet-go/parquet-go/compress"
	"github.com/parquet-go/parquet-go/deprecated"
	"github.com/parquet-go/parquet-go/encoding"
	"github.com/parquet-go/parquet-go/encoding/bitpacked"
	"github.com/parquet-go/parquet-go/encoding/rle"
	"github.com/parquet-go/parquet-go/encoding/thrift"
	"github.com/parquet-go/parquet-go/format"
)

// ReadInt96MillisColumn reads an INT96 TIMESTAMP(MILLIS) annotation column.
//
// parquet-go's OpenFile remaps TIMESTAMP logical types to INT64 regardless of
// the footer's physical type, so Schema().Lookup and Pages() fail with
// "cannot decode INT64 from input of size 12". This helper is a workaround for
// files written by this package (PLAIN INT96, DataPage V1 or V2). Dictionary
// pages and non-PLAIN encodings are rejected.
//
// Null annotation slots are nil. Optional, non-repeated columns are row-aligned
// (result length matches File.NumRows()). LIST/repeated parents are
// definition-level aligned: one entry per list element, which can exceed
// NumRows. A single path element matches a unique leaf name; pass the full
// schema path when names collide (for example effectivePeriod.__start_start vs
// valuePeriod.__start_start).
func ReadInt96MillisColumn(r io.ReaderAt, size int64, path ...string) ([]*time.Time, error) {
	file, err := parquet.OpenFile(r, size)
	if err != nil {
		return nil, fmt.Errorf("parquetfhir: open INT96 file: %w", err)
	}
	resolved, leaf, err := ResolveInt96Column(file.Schema(), path...)
	if err != nil {
		return nil, err
	}
	colIndex := leaf.ColumnIndex
	meta := file.Metadata()

	var out []*time.Time
	for _, rg := range meta.RowGroups {
		if colIndex >= len(rg.Columns) {
			return nil, fmt.Errorf("parquetfhir: row group missing column %s", strings.Join(resolved, "."))
		}
		chunk := rg.Columns[colIndex]
		if chunk.MetaData.Type != format.Int96 {
			return nil, fmt.Errorf("parquetfhir: column %s physical type is not INT96", strings.Join(resolved, "."))
		}
		values, err := decodeInt96Chunk(r, chunk, byte(leaf.MaxRepetitionLevel), byte(leaf.MaxDefinitionLevel))
		if err != nil {
			return nil, fmt.Errorf("parquetfhir: decode INT96 column %s: %w", strings.Join(resolved, "."), err)
		}
		out = append(out, values...)
	}
	return out, nil
}

func int96MillisToTime(v deprecated.Int96) time.Time {
	return time.UnixMilli(v.Int64()).UTC()
}

// ResolveInt96Column maps a unique leaf name or a full schema path to a parquet
// leaf. A single element matches one unique trailing name; collisions require
// the full path (for example effectivePeriod.__start_start).
func ResolveInt96Column(schema *parquet.Schema, path ...string) ([]string, parquet.LeafColumn, error) {
	if schema == nil {
		return nil, parquet.LeafColumn{}, fmt.Errorf("parquetfhir: schema is required")
	}
	if len(path) == 0 {
		return nil, parquet.LeafColumn{}, fmt.Errorf("parquetfhir: column path is required")
	}
	if len(path) > 1 {
		leaf, ok := schema.Lookup(path...)
		if !ok {
			return nil, parquet.LeafColumn{}, fmt.Errorf("parquetfhir: column %s not found", strings.Join(path, "."))
		}
		return path, leaf, nil
	}
	name := path[0]
	var matches [][]string
	for _, col := range schema.Columns() {
		if len(col) > 0 && col[len(col)-1] == name {
			matches = append(matches, col)
		}
	}
	if len(matches) == 0 {
		return nil, parquet.LeafColumn{}, fmt.Errorf("parquetfhir: column %q not found", name)
	}
	if len(matches) > 1 {
		listed := make([]string, len(matches))
		for i, col := range matches {
			listed[i] = strings.Join(col, ".")
		}
		return nil, parquet.LeafColumn{}, fmt.Errorf("parquetfhir: column %q is ambiguous; use a full path (matches %s)", name, strings.Join(listed, ", "))
	}
	leaf, ok := schema.Lookup(matches[0]...)
	if !ok {
		return nil, parquet.LeafColumn{}, fmt.Errorf("parquetfhir: column %q not found", name)
	}
	return matches[0], leaf, nil
}

func decodeInt96Chunk(r io.ReaderAt, chunk format.ColumnChunk, maxRep, maxDef byte) ([]*time.Time, error) {
	// Page layout is decoded here because parquet-go remaps TIMESTAMP pages to
	// INT64. Keep this in sync with GenericWriter PLAIN INT96 output in
	// row_writer.go (DataPage V1 and V2).
	offset := chunk.MetaData.DataPageOffset
	if chunk.MetaData.DictionaryPageOffset != 0 {
		offset = chunk.MetaData.DictionaryPageOffset
	}
	size := chunk.MetaData.TotalCompressedSize
	if size <= 0 {
		return nil, nil
	}
	buf := make([]byte, size)
	if _, err := io.ReadFull(io.NewSectionReader(r, offset, size), buf); err != nil {
		return nil, err
	}

	codec := parquet.LookupCompressionCodec(chunk.MetaData.Codec)
	reader := newBytesCursor(buf)
	decoder := thrift.NewDecoder((&thrift.CompactProtocol{}).NewReader(reader))
	var values []*time.Time
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
			return nil, fmt.Errorf("dictionary INT96 pages are not supported")
		case format.DataPageV2:
			pageValues, err := decodeInt96DataPageV2(header, page, codec, maxRep, maxDef)
			if err != nil {
				return nil, err
			}
			values = append(values, pageValues...)
		case format.DataPage:
			pageValues, err := decodeInt96DataPageV1(header, page, codec, maxRep, maxDef)
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

func decodeInt96DataPageV2(header format.PageHeader, page []byte, codec compress.Codec, maxRep, maxDef byte) ([]*time.Time, error) {
	if !header.DataPageHeaderV2.Valid {
		return nil, fmt.Errorf("missing DataPageHeaderV2")
	}
	v2 := header.DataPageHeaderV2.V
	if err := requirePlainInt96(v2.Encoding); err != nil {
		return nil, err
	}
	numValues := int(v2.NumValues)
	data := page
	if int(v2.RepetitionLevelsByteLength)+int(v2.DefinitionLevelsByteLength) > len(data) {
		return nil, fmt.Errorf("INT96 v2 page shorter than level prefix")
	}
	var err error
	if maxRep > 0 {
		data, err = skipOrDecodeV2Levels(data, int(v2.RepetitionLevelsByteLength), numValues, maxRep)
		if err != nil {
			return nil, fmt.Errorf("repetition levels: %w", err)
		}
	} else if v2.RepetitionLevelsByteLength > 0 {
		data = data[v2.RepetitionLevelsByteLength:]
	}
	var defLevels []byte
	if maxDef > 0 {
		defLevels, data, err = decodeV2Levels(data, int(v2.DefinitionLevelsByteLength), numValues, maxDef)
		if err != nil {
			return nil, fmt.Errorf("definition levels: %w", err)
		}
	} else if v2.DefinitionLevelsByteLength > 0 {
		data = data[v2.DefinitionLevelsByteLength:]
	}
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
	present, err := decodePlainInt96(data)
	if err != nil {
		return nil, err
	}
	return zipInt96Times(present, defLevels, maxDef)
}

func decodeInt96DataPageV1(header format.PageHeader, page []byte, codec compress.Codec, maxRep, maxDef byte) ([]*time.Time, error) {
	if !header.DataPageHeader.Valid {
		return nil, fmt.Errorf("missing DataPageHeader")
	}
	v1 := header.DataPageHeader.V
	if err := requirePlainInt96(v1.Encoding); err != nil {
		return nil, err
	}
	data := page
	if codec != nil {
		decoded, err := codec.Decode(nil, data)
		if err != nil {
			return nil, err
		}
		data = decoded
	}
	numValues := int(v1.NumValues)
	var err error
	if maxRep > 0 {
		data, err = consumeV1Levels(data, numValues, v1.RepetitionLevelEncoding, maxRep)
		if err != nil {
			return nil, fmt.Errorf("repetition levels: %w", err)
		}
	}
	var defLevels []byte
	if maxDef > 0 {
		defLevels, data, err = decodeV1Levels(data, numValues, v1.DefinitionLevelEncoding, maxDef)
		if err != nil {
			return nil, fmt.Errorf("definition levels: %w", err)
		}
	}
	present, err := decodePlainInt96(data)
	if err != nil {
		return nil, err
	}
	return zipInt96Times(present, defLevels, maxDef)
}

func requirePlainInt96(enc format.Encoding) error {
	if enc != format.Plain {
		return fmt.Errorf("INT96 encoding %s is not supported", enc)
	}
	return nil
}

func skipOrDecodeV2Levels(data []byte, length, numValues int, maxLevel byte) ([]byte, error) {
	if length > len(data) {
		return nil, io.ErrUnexpectedEOF
	}
	if maxLevel == 0 {
		return data[length:], nil
	}
	_, rest, err := decodeV2Levels(data, length, numValues, maxLevel)
	return rest, err
}

func decodeV2Levels(data []byte, length, numValues int, maxLevel byte) ([]byte, []byte, error) {
	if length > len(data) {
		return nil, nil, io.ErrUnexpectedEOF
	}
	levels, err := decodeLevelBytes(newLevelEncoding(format.RLE, maxLevel), numValues, data[:length])
	if err != nil {
		return nil, nil, err
	}
	return levels, data[length:], nil
}

func consumeV1Levels(data []byte, numValues int, enc format.Encoding, maxLevel byte) ([]byte, error) {
	_, rest, err := decodeV1Levels(data, numValues, enc, maxLevel)
	return rest, err
}

func decodeV1Levels(data []byte, numValues int, enc format.Encoding, maxLevel byte) ([]byte, []byte, error) {
	if len(data) < 4 {
		return nil, nil, io.ErrUnexpectedEOF
	}
	n := int(binary.LittleEndian.Uint32(data))
	start, end := 4, 4+n
	if end > len(data) {
		return nil, nil, io.ErrUnexpectedEOF
	}
	levels, err := decodeLevelBytes(newLevelEncoding(enc, maxLevel), numValues, data[start:end])
	if err != nil {
		return nil, nil, err
	}
	return levels, data[end:], nil
}

func newLevelEncoding(enc format.Encoding, maxLevel byte) encoding.Encoding {
	width := bits.Len8(maxLevel)
	if width < 1 {
		width = 1
	}
	switch enc {
	case format.BitPacked:
		return &bitpacked.Encoding{BitWidth: width}
	default:
		return &rle.Encoding{BitWidth: width}
	}
}

func decodeLevelBytes(enc encoding.Encoding, numValues int, src []byte) ([]byte, error) {
	decoded, err := enc.DecodeLevels(make([]byte, 0, numValues), src)
	if err != nil {
		return nil, err
	}
	if len(decoded) < numValues {
		return nil, fmt.Errorf("decoded %d levels, want %d", len(decoded), numValues)
	}
	return decoded[:numValues], nil
}

func zipInt96Times(values []deprecated.Int96, defLevels []byte, maxDef byte) ([]*time.Time, error) {
	if len(defLevels) == 0 {
		out := make([]*time.Time, len(values))
		for i, v := range values {
			t := int96MillisToTime(v)
			out[i] = &t
		}
		return out, nil
	}
	out := make([]*time.Time, len(defLevels))
	vi := 0
	for i, def := range defLevels {
		if def != maxDef {
			continue
		}
		if vi >= len(values) {
			return nil, fmt.Errorf("INT96 value underflow at definition level %d", i)
		}
		t := int96MillisToTime(values[vi])
		out[i] = &t
		vi++
	}
	if vi != len(values) {
		return nil, fmt.Errorf("INT96 unused values: decoded %d, used %d", len(values), vi)
	}
	return out, nil
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
