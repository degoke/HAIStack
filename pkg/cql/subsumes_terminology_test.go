package cql

import (
	"context"
	"testing"

	"github.com/degoke/health-ai-stack/pkg/fhirpath"
)

type fakeSubsumption struct {
	tree map[string]string // child -> parent
}

func (f fakeSubsumption) MemberOf(context.Context, string, string, string) (bool, error) {
	return false, nil
}

func (f fakeSubsumption) Subsumes(_ context.Context, _ string, broad, narrow string) (bool, error) {
	for cur := narrow; cur != ""; {
		if cur == broad {
			return true, nil
		}
		cur = f.tree[cur]
	}
	return false, nil
}

func TestSubsumesStrictWhenTerminologyWired(t *testing.T) {
	term := fakeSubsumption{tree: map[string]string{}}
	eng, err := NewEngine(Config{Terminology: term})
	if err != nil {
		t.Fatal(err)
	}
	got, err := eng.Eval(context.Background(),
		"{code:'chi', system:'http://cs'} subsumes {code:'child', system:'http://cs'}",
		EvalContext{})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != false {
		t.Fatalf("expected false without heuristic when terminology is wired: %#v", got)
	}
}

func TestSubsumesUsesTerminologyValidator(t *testing.T) {
	term := fakeSubsumption{tree: map[string]string{"child": "parent", "parent": "root"}}
	eng, err := NewEngine(Config{Terminology: term})
	if err != nil {
		t.Fatal(err)
	}
	got, err := eng.Eval(context.Background(),
		"{code:'parent', system:'http://cs'} subsumes {code:'child', system:'http://cs'}",
		EvalContext{})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != true {
		t.Fatalf("terminology subsumes: %#v", got)
	}
	// Prefix heuristic would also match; use codes that fail heuristic but pass tree.
	term2 := fakeSubsumption{tree: map[string]string{"8867-4": "8867"}}
	eng2, err := NewEngine(Config{Terminology: term2})
	if err != nil {
		t.Fatal(err)
	}
	got, err = eng2.Eval(context.Background(),
		"{code:'8867', system:'http://loinc.org'} subsumes {code:'8867-4', system:'http://loinc.org'}",
		EvalContext{})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != true {
		t.Fatalf("LOINC-style hierarchy via TX: %#v", got)
	}
}

func TestTerminologyValidatorsAdapterSubsumes(t *testing.T) {
	term := fhirpath.TerminologyValidatorsAdapter(nil, func(context.Context, string, string, string) (bool, error) {
		return true, nil
	})
	sub, ok := term.(fhirpath.SubsumptionValidator)
	if !ok {
		t.Fatal("expected SubsumptionValidator")
	}
	ok, err := sub.Subsumes(context.Background(), "http://cs", "a", "b")
	if err != nil || !ok {
		t.Fatalf("Subsumes: ok=%v err=%v", ok, err)
	}
}
