package subscriptions

import (
	"errors"
)

var (
	ErrNilStore          = errors.New("subscriptions: store is nil")
	ErrNilEngine         = errors.New("subscriptions: fhirpath engine is nil")
	ErrNotFound          = errors.New("subscriptions: not found")
	ErrInvalidTrigger    = errors.New("subscriptions: invalid trigger")
	ErrInvalidChannel    = errors.New("subscriptions: invalid channel")
	ErrUnsupportedFHIR   = errors.New("subscriptions: unsupported FHIR subscription shape")
	ErrUnknownHandler    = errors.New("subscriptions: unknown local handler")
	ErrDuplicateDelivery = errors.New("subscriptions: delivery already completed")
)
