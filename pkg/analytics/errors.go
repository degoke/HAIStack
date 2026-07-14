package analytics

import "errors"

var (
	// ErrUnsupportedDestination is returned when a run request has no valid target.
	ErrUnsupportedDestination = errors.New("analytics: unsupported destination configuration")
	// ErrUnsupportedMode is returned when the run mode is not recognized.
	ErrUnsupportedMode = errors.New("analytics: unsupported run mode")
	// ErrUnsupportedView is returned when the view is not in the first-milestone set.
	ErrUnsupportedView = errors.New("analytics: unsupported view")
	// ErrSinkNotImplemented is returned by deferred sink backends.
	ErrSinkNotImplemented = errors.New("analytics: sink not implemented")
	// ErrMissingExecutor is returned when Runner is constructed without a view executor.
	ErrMissingExecutor = errors.New("analytics: missing view executor")
)
