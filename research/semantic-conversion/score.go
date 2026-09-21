// Command semantic-conversion scores the R4→R5 paired corpus.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/degoke/health-ai-stack/pkg/fhirpath"
	proto "github.com/degoke/health-ai-stack/pkg/proto"
	"github.com/degoke/health-ai-stack/pkg/types"
)

// ScoreReport is the conversion-corpus evaluation output.
type ScoreReport struct {
	Track      string         `json:"track"`
	Pairs      int            `json:"pairs"`
	Passed     int            `json:"passed"`
	Failed     int            `json:"failed"`
	ByCategory map[string]int `json:"byCategory"`
	ByType     map[string]int `json:"byResourceType"`
	LossFlags  int            `json:"informationLossFlags"`
	Failures   []PairFailure  `json:"failures,omitempty"`
}

// PairFailure records why one pair did not score cleanly.
type PairFailure struct {
	ID     string   `json:"id"`
	Errors []string `json:"errors"`
}

// ScoreAll evaluates every corpus pair.
func ScoreAll(ctx context.Context) (*ScoreReport, error) {
	engine, err := fhirpath.NewEngine(fhirpath.Config{})
	if err != nil {
		return nil, err
	}
	codec := proto.NewGoogleR4Codec()
	pairs := Corpus()
	report := &ScoreReport{
		Track:      "B",
		Pairs:      len(pairs),
		ByCategory: map[string]int{},
		ByType:     map[string]int{},
	}
	for _, pair := range pairs {
		report.ByCategory[pair.Category]++
		report.ByType[pair.ResourceType]++
		report.LossFlags += len(pair.InformationLoss)
		if errs := scorePair(ctx, codec, engine, pair); len(errs) > 0 {
			report.Failed++
			report.Failures = append(report.Failures, PairFailure{ID: pair.ID, Errors: errs})
			continue
		}
		report.Passed++
	}
	return report, nil
}

func scorePair(ctx context.Context, codec *proto.GoogleR4Codec, engine fhirpath.Engine, pair Pair) []string {
	var errs []string
	r4proto, protoErr := codec.ParseJSONToEnvelope(pair.ResourceType, pair.R4)
	if protoErr != nil {
		errs = append(errs, fmt.Sprintf("r4 proto parse: %v", protoErr))
	}
	r4env, err := types.NewJSONCodec().ParseJSON(pair.ResourceType, pair.R4)
	if err != nil {
		errs = append(errs, fmt.Sprintf("r4 json: %v", err))
	}
	r5env, err := types.NewJSONCodec().ParseJSON(pair.ResourceType, pair.R5)
	if err != nil {
		errs = append(errs, fmt.Sprintf("r5 json: %v", err))
	}
	if r4env != nil && r5env != nil {
		for _, path := range pair.StablePaths {
			if !structuralEqual(r4env, r5env, path) {
				errs = append(errs, fmt.Sprintf("structural path %q differs", path))
			}
		}
	}
	for _, a := range pair.Assertions {
		env := r4env
		if strings.EqualFold(a.Version, "r5") {
			env = r5env
		}
		if env == nil {
			errs = append(errs, fmt.Sprintf("assertion %s %q: missing resource", a.Version, a.Expr))
			continue
		}
		got, err := evalAssertion(ctx, engine, r4proto, env, a)
		if err != nil {
			errs = append(errs, fmt.Sprintf("assertion %s %q: %v", a.Version, a.Expr, err))
			continue
		}
		if got != a.Want {
			errs = append(errs, fmt.Sprintf("assertion %s %q: got %v want %v", a.Version, a.Expr, got, a.Want))
		}
	}
	if pair.Category == "information_loss" && len(pair.InformationLoss) == 0 {
		errs = append(errs, "information_loss category requires flags")
	}
	return errs
}

func evalAssertion(ctx context.Context, engine fhirpath.Engine, r4proto, env *types.ResourceEnvelope, a Assertion) (bool, error) {
	useFHIRPath := r4proto != nil && strings.EqualFold(a.Version, "r4") && !strings.Contains(a.Expr, "ofType")
	if useFHIRPath {
		got, err := engine.EvalBool(ctx, a.Expr, r4proto)
		if err == nil {
			return got, nil
		}
	}
	return evalJSONBool(env.JSON, a.Expr)
}

func evalJSONBool(raw []byte, expr string) (bool, error) {
	var obj map[string]any
	if err := json.Unmarshal(raw, &obj); err != nil {
		return false, err
	}
	expr = strings.TrimSpace(expr)
	wantCount := -1
	wantEqual := ""
	exists := strings.HasSuffix(expr, ".exists()")
	switch {
	case exists:
		expr = strings.TrimSuffix(expr, ".exists()")
	case strings.Contains(expr, ".count() = "):
		parts := strings.Split(expr, ".count() = ")
		if len(parts) != 2 {
			return false, fmt.Errorf("unsupported count expression %q", expr)
		}
		expr = parts[0]
		n, err := strconv.Atoi(strings.TrimSpace(parts[1]))
		if err != nil {
			return false, err
		}
		wantCount = n
	case strings.Contains(expr, " = "):
		parts := strings.SplitN(expr, " = ", 2)
		expr = strings.TrimSpace(parts[0])
		wantEqual = strings.Trim(strings.TrimSpace(parts[1]), "'\"")
	default:
		return false, fmt.Errorf("unsupported assertion %q", expr)
	}
	path := rewriteChoiceTypes(expr)
	values := walkJSON(obj, path)
	switch {
	case exists:
		return len(values) > 0, nil
	case wantCount >= 0:
		return len(values) == wantCount, nil
	default:
		for _, v := range values {
			if fmt.Sprint(v) == wantEqual {
				return true, nil
			}
		}
		return false, nil
	}
}

func rewriteChoiceTypes(expr string) []string {
	repl := []struct{ from, to string }{
		{".ofType(boolean)", "Boolean"},
		{".ofType(dateTime)", "DateTime"},
		{".ofType(Quantity)", "Quantity"},
		{".ofType(CodeableConcept)", "CodeableConcept"},
		{".ofType(Age)", "Age"},
	}
	out := expr
	for _, r := range repl {
		if strings.Contains(out, r.from) {
			out = strings.ReplaceAll(out, r.from, r.to)
		}
	}
	out = strings.ReplaceAll(out, ".first()", "")
	parts := strings.Split(out, ".")
	if len(parts) > 0 {
		parts = parts[1:] // drop resource type
	}
	return parts
}

func walkJSON(root any, path []string) []any {
	cur := []any{root}
	for _, p := range path {
		if p == "" {
			continue
		}
		next := make([]any, 0)
		for _, node := range cur {
			switch n := node.(type) {
			case map[string]any:
				if v, ok := n[p]; ok {
					next = append(next, flatten(v)...)
				}
				// Choice types: deceasedBoolean when path is deceasedBoolean after rewrite
			case []any:
				for _, item := range n {
					if m, ok := item.(map[string]any); ok {
						if v, ok := m[p]; ok {
							next = append(next, flatten(v)...)
						}
					}
				}
			}
		}
		cur = next
	}
	return cur
}

func flatten(v any) []any {
	if arr, ok := v.([]any); ok {
		return arr
	}
	if v == nil {
		return nil
	}
	return []any{v}
}

func structuralEqual(left, right *types.ResourceEnvelope, path string) bool {
	lv, lok := left.Field(path)
	rv, rok := right.Field(path)
	if !lok && !rok {
		return true
	}
	if lok != rok {
		return false
	}
	lb, err1 := json.Marshal(lv)
	rb, err2 := json.Marshal(rv)
	if err1 != nil || err2 != nil {
		return false
	}
	return string(lb) == string(rb)
}

func main() {
	report, err := ScoreAll(context.Background())
	if err != nil {
		fmt.Fprintf(os.Stderr, "semantic-conversion: %v\n", err)
		os.Exit(1)
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(report); err != nil {
		fmt.Fprintf(os.Stderr, "semantic-conversion: %v\n", err)
		os.Exit(1)
	}
	if report.Pairs < 50 {
		fmt.Fprintf(os.Stderr, "semantic-conversion: need ≥50 pairs, got %d\n", report.Pairs)
		os.Exit(1)
	}
	if report.Failed > 0 {
		os.Exit(1)
	}
}
