package terminology

import (
	"context"
	"testing"
)

type subsumesStub struct {
	ok bool
}

func (subsumesStub) Lookup(context.Context, LookupRequest) (*LookupResult, error) {
	return nil, nil
}
func (subsumesStub) Expand(context.Context, ExpandRequest) (*Expansion, error) {
	return nil, nil
}
func (subsumesStub) ValidateCode(context.Context, ValidateCodeRequest) (*ValidationResult, error) {
	return nil, nil
}
func (s subsumesStub) Subsumes(context.Context, SubsumesRequest) (bool, error) {
	return s.ok, nil
}

func TestChainSubsumesTriesNextProvider(t *testing.T) {
	chain := Chain{Providers: []Provider{
		subsumesStub{ok: false},
		subsumesStub{ok: true},
	}}
	ok, err := chain.Subsumes(context.Background(), SubsumesRequest{
		System: "http://cs", BroadCode: "a", NarrowCode: "b",
	})
	if err != nil || !ok {
		t.Fatalf("chain subsumes: ok=%v err=%v", ok, err)
	}
}
