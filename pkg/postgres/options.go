package postgres

// Option configures an opened Postgres database pool.
type Option func(*options)

type options struct {
	maxConns int32
	minConns int32
	schema   string
}

// WithMaxConns sets the maximum number of connections in the pool.
func WithMaxConns(n int32) Option {
	return func(o *options) {
		o.maxConns = n
	}
}

// WithMinConns sets the minimum number of connections in the pool.
func WithMinConns(n int32) Option {
	return func(o *options) {
		o.minConns = n
	}
}

// WithSchema sets the Postgres schema for haistack tables. Defaults to public.
func WithSchema(name string) Option {
	return func(o *options) {
		o.schema = name
	}
}

func defaultOptions() options {
	return options{
		maxConns: 10,
		minConns: 1,
		schema:   defaultSchema,
	}
}
