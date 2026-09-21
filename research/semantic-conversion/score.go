// Command semantic-conversion scores the R4→R5 paired corpus.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
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
	Differing  int            `json:"differingPairs"`
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
		if string(pair.R4) != string(pair.R5) {
			report.Differing++
		}
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
	if r4proto != nil {
		for _, a := range pair.R4FHIRPath {
			got, err := engine.EvalBool(ctx, a.Expr, r4proto)
			if err != nil {
				errs = append(errs, fmt.Sprintf("r4 fhirpath %q: %v", a.Expr, err))
				continue
			}
			if got != a.Want {
				errs = append(errs, fmt.Sprintf("r4 fhirpath %q: got %v want %v", a.Expr, got, a.Want))
			}
		}
	} else if len(pair.R4FHIRPath) > 0 {
		errs = append(errs, "r4 fhirpath: missing proto envelope")
	}
	if r5env != nil {
		for _, c := range pair.R5JSON {
			got, err := evalJSONCheck(r5env.JSON, c)
			if err != nil {
				errs = append(errs, fmt.Sprintf("r5 json %s %q: %v", c.Op, c.Path, err))
				continue
			}
			if !got {
				errs = append(errs, fmt.Sprintf("r5 json %s %q: not satisfied (value=%v)", c.Op, c.Path, c.Value))
			}
		}
	} else if len(pair.R5JSON) > 0 {
		errs = append(errs, "r5 json checks: missing resource")
	}
	if pair.Category == "information_loss" && len(pair.InformationLoss) == 0 {
		errs = append(errs, "information_loss category requires flags")
	}
	return errs
}

func evalJSONCheck(raw []byte, c JSONCheck) (bool, error) {
	var obj map[string]any
	if err := json.Unmarshal(raw, &obj); err != nil {
		return false, err
	}
	values := walkJSON(obj, strings.Split(c.Path, "."))
	switch c.Op {
	case "exists":
		return len(values) > 0, nil
	case "missing":
		return len(values) == 0, nil
	case "count":
		n, ok := asInt(c.Value)
		if !ok {
			return false, fmt.Errorf("count value %v is not an integer", c.Value)
		}
		return len(values) == n, nil
	case "equals":
		want := fmt.Sprint(c.Value)
		for _, v := range values {
			if fmt.Sprint(v) == want {
				return true, nil
			}
		}
		return false, nil
	default:
		return false, fmt.Errorf("unsupported json check op %q", c.Op)
	}
}

func asInt(v any) (int, bool) {
	switch n := v.(type) {
	case int:
		return n, true
	case int64:
		return int(n), true
	case float64:
		return int(n), true
	default:
		return 0, false
	}
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
	if report.Differing < 30 {
		fmt.Fprintf(os.Stderr, "semantic-conversion: need ≥30 differing R4/R5 pairs, got %d\n", report.Differing)
		os.Exit(1)
	}
	if report.Failed > 0 {
		os.Exit(1)
	}
}
