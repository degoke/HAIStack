package ai

import (
	"context"
	"testing"

	"github.com/degoke/haistack/pkg/fhirpath"
	"github.com/degoke/haistack/pkg/types"
)

func TestFHIRPathKeywordElementNames(t *testing.T) {
	engine, err := fhirpath.NewEngine(fhirpath.Config{})
	if err != nil {
		t.Fatal(err)
	}
	data := []byte(`{"resourceType":"Patient","id":"1","text":{"status":"generated","div":"<div>x</div>"}}`)
	res, err := types.NewJSONCodec().ParseJSON("Patient", data)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	for _, expr := range []string{
		"text.children('div')",
		"text.`div`",
		"text.%div",
		"text.select('$.div')",
	} {
		_, compileErr := engine.Compile(expr)
		t.Logf("compile %q: %v", expr, compileErr)
		if compileErr == nil {
			out, evalErr := engine.Eval(ctx, expr, res)
			t.Logf("eval %q: n=%d err=%v", expr, len(out), evalErr)
		}
	}
}
