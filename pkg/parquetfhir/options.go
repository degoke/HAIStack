package parquetfhir

// WriteOption configures Parquet-on-FHIR resource encoding.
type WriteOption func(*writeConfig)

type writeConfig struct {
	timestampEncoding TimestampEncoding
	rowGroupSize      int
}

// WithTimestampEncoding selects INT64 (default) or INT96 date annotation columns.
func WithTimestampEncoding(enc TimestampEncoding) WriteOption {
	return func(cfg *writeConfig) {
		if cfg != nil {
			cfg.timestampEncoding = enc
		}
	}
}

func newWriteConfig(opts []WriteOption) writeConfig {
	cfg := writeConfig{
		timestampEncoding: TimestampEncodingInt64,
		rowGroupSize:      DefaultRowGroupSize,
	}
	for _, opt := range opts {
		if opt != nil {
			opt(&cfg)
		}
	}
	cfg.timestampEncoding = NormalizeTimestampEncoding(cfg.timestampEncoding)
	if cfg.rowGroupSize <= 0 {
		cfg.rowGroupSize = DefaultRowGroupSize
	}
	return cfg
}
