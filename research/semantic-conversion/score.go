// Command semantic-conversion scores the R4→R5 paired corpus.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/degoke/haistack/pkg/fhirpath"
	proto "github.com/degoke/haistack/pkg/proto"
	"github.com/degoke/haistack/pkg/types"
)

// ScoreReport is the conversion-corpus evaluation output.
// Mode is "catalogue": the scorer checks embedded pairs. It does not convert
// R4 JSON into R5 (there is no R5 codec / conversion pipeline).
type ScoreReport struct {
	Track      string         `json:"track"`
	Mode       string         `json:"mode"`
	Converter  bool           `json:"converter"`
	Pairs      int            `json:"pairs"`
	Passed     int            `json:"passed"`
	Failed     int            `json:"failed"`
	Differing  int            `json:"differingPairs"`
	ByCategory map[string]int `json:"byCategory"`
	ByType     map[string]int `json:"byResourceType"`
	// LossFlags is the count of declared pair.InformationLoss strings, not
	// a scored metric and not byCategory["information_loss"].
	LossFlags int           `json:"informationLossFlags"`
	Failures  []PairFailure `json:"failures,omitempty"`
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
		Mode:       "catalogue",
		Converter:  false,
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
	if len(pair.InformationLoss) > 0 {
		if r4env == nil {
			errs = append(errs, "informationLoss: missing R4 resource")
		} else {
			for _, flag := range pair.InformationLoss {
				present, err := informationLossPresent(r4env.JSON, pair.ResourceType, flag)
				if err != nil {
					errs = append(errs, fmt.Sprintf("informationLoss %q on R4: %v", flag, err))
					continue
				}
				if !present {
					errs = append(errs, fmt.Sprintf("informationLoss %q not present on R4", flag))
				}
			}
		}
		if r5env == nil {
			errs = append(errs, "informationLoss: missing R5 resource")
		} else {
			for _, flag := range pair.InformationLoss {
				absent, err := informationLossAbsent(r5env.JSON, pair.ResourceType, flag)
				if err != nil {
					errs = append(errs, fmt.Sprintf("informationLoss %q on R5: %v", flag, err))
					continue
				}
				if !absent {
					errs = append(errs, fmt.Sprintf("informationLoss %q still present on R5", flag))
				}
			}
		}
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

func informationLossPresent(raw []byte, resourceType, flag string) (bool, error) {
	absent, err := informationLossAbsent(raw, resourceType, flag)
	if err != nil {
		return false, err
	}
	return !absent, nil
}

func informationLossAbsent(raw []byte, resourceType, flag string) (bool, error) {
	var obj map[string]any
	if err := json.Unmarshal(raw, &obj); err != nil {
		return false, err
	}
	path := strings.TrimSpace(flag)
	if path == "" {
		return false, fmt.Errorf("empty flag")
	}
	if resourceType != "" && strings.HasPrefix(path, resourceType+".") {
		path = strings.TrimPrefix(path, resourceType+".")
	}
	if strings.HasPrefix(path, "extension[") && strings.HasSuffix(path, "]") {
		url := strings.TrimSuffix(strings.TrimPrefix(path, "extension["), "]")
		return extensionURLAbsent(obj, url), nil
	}
	return len(walkJSON(obj, strings.Split(path, "."))) == 0, nil
}

func extensionURLAbsent(obj map[string]any, url string) bool {
	raw, ok := obj["extension"]
	if !ok || raw == nil {
		return true
	}
	arr, ok := raw.([]any)
	if !ok {
		return true
	}
	for _, item := range arr {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if fmt.Sprint(m["url"]) == url {
			return false
		}
	}
	return true
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
