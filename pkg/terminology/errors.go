package terminology

import "errors"

// ErrExpansionNotFound indicates no terminology provider could expand the ValueSet.
var ErrExpansionNotFound = errors.New("ValueSet not found")

// ErrExpansionTooCostly indicates the expansion exceeds configured limits.
var ErrExpansionTooCostly = errors.New("expansion too costly")
