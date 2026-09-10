package terminology

import (
	"context"
	"testing"
)

type costlyLocal struct{}

func (costlyLocal) Lookup(context.Context, LookupRequest) (*LookupResult, error) {
	return nil, nil
}

func (costlyLocal) Expand(context.Context, ExpandRequest) (*Expansion, error) {
	return nil, ErrExpansionTooCostly
}

func (costlyLocal) ValidateCode(context.Context, ValidateCodeRequest) (*ValidationResult, error) {
	return nil, nil
}

type remoteExpand struct{}

func (remoteExpand) Lookup(context.Context, LookupRequest) (*LookupResult, error) {
	return nil, nil
}

func (remoteExpand) Expand(context.Context, ExpandRequest) (*Expansion, error) {
	return &Expansion{Total: 1, Contains: []Coding{{Code: "remote"}}}, nil
}

func (remoteExpand) ValidateCode(context.Context, ValidateCodeRequest) (*ValidationResult, error) {
	return nil, nil
}

func TestChainExpandTooCostlyIsTerminal(t *testing.T) {
	chain := Chain{Providers: []Provider{costlyLocal{}, remoteExpand{}}}
	ex, err := chain.Expand(context.Background(), ExpandRequest{URL: "urn:vs"})
	if err != ErrExpansionTooCostly || ex != nil {
		t.Fatalf("expand=%+v err=%v", ex, err)
	}
}
