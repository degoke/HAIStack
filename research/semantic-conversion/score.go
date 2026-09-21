package semanticconversion

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/degoke/health-ai-stack/pkg/fhirpath"
	"github.com/degoke/health-ai-stack/pkg/types"
)

const (
	engineFHIRPath = "fhirpath"
	engineJSONPath = "json-path"
)

// PairScore is the per-instance conversion score.
type PairScore struct {
	ID              string   `json:"id"`
	ResourceType    string   `json:"resourceType"`
	Category        string   `json:"category"`
	StructuralOK    bool     `json:"structuralOk"`
	SemanticOK      bool     `json:"semanticOk"`
	R4Engine        string   `json:"r4Engine,omitempty"`
	R5Engine        string   `json:"r5Engine,omitempty"`
	InformationLoss []string `json:"informationLoss,omitempty"`
	Errors          []string `json:"errors,omitempty"`
}

// Report summarizes corpus scoring.
type Report struct {
	Pairs      int            `json:"pairs"`
	Structural int            `json:"structuralPassed"`
	Semantic   int            `json:"semanticPassed"`
	Failed     int            `json:"failed"`
	ByCategory map[string]int `json:"byCategory"`
	Scores     []PairScore    `json:"scores"`
}

// ScoreCorpus converts each R4 instance with ConvertR4ToR5 and requires that
// output to equal authored gold R5 except gold-only meta.source. Convert does
// not produce gold; testdata is the oracle. Dropping copy-through fields (id,
// subject, …) or remapped fields (reason, participant) fails structural.
//
// Semantic R4 checks use pkg/fhirpath. Instances the R4 protobuf codec cannot
// load (unknown fields such as Patient.animal, singleton JSON for 0..*
// interpretation) fall back to the JSON-path subset. Semantic R5 checks always
// use that JSON-path subset because no production R5 codec exists yet, and they
// run against the converted payload rather than the gold file.
//
// Information-loss flags declared on a pair must appear in the loss list
// returned by ConvertR4ToR5. Declared flags are not merged into that list
// before the check.
func ScoreCorpus(pairs []Pair) (Report, error) {
	fp, err := fhirpath.NewEngine(fhirpath.Config{})
	if err != nil {
		return Report{}, err
	}
	env := scoreEnv{
		ctx:   context.Background(),
		fp:    fp,
		codec: types.NewJSONCodec(),
	}
	report := Report{ByCategory: map[string]int{}}
	for _, pair := range pairs {
		score := scorePair(env, pair)
		report.Pairs++
		report.ByCategory[pair.Category]++
		if score.StructuralOK {
			report.Structural++
		}
		if score.SemanticOK {
			report.Semantic++
		}
		if !score.StructuralOK || !score.SemanticOK {
			report.Failed++
		}
		report.Scores = append(report.Scores, score)
	}
	return report, nil
}

type scoreEnv struct {
	ctx   context.Context
	fp    fhirpath.Engine
	codec *types.JSONCodec
}

func scorePair(env scoreEnv, pair Pair) PairScore {
	score := PairScore{
		ID:           pair.ID,
		ResourceType: pair.ResourceType,
		Category:     pair.Category,
		R4Engine:     engineFHIRPath,
		R5Engine:     engineJSONPath,
	}
	got, loss, err := ConvertR4ToR5(pair.ResourceType, pair.R4)
	if err != nil {
		score.Errors = append(score.Errors, err.Error())
		return score
	}
	score.InformationLoss = sortedCopy(loss)
	ok, msg, err := structuralOK(pair, got)
	if err != nil {
		score.Errors = append(score.Errors, err.Error())
		return score
	}
	score.StructuralOK = ok
	if !ok && msg != "" {
		score.Errors = append(score.Errors, msg)
	}

	score.SemanticOK = true
	for _, as := range pair.Assertions {
		if as.R4 != "" {
			engine, err := assertR4(env, pair.ResourceType, pair.R4, as.R4, as.Want)
			if engine == engineJSONPath {
				score.R4Engine = engineJSONPath
			}
			if err != nil {
				score.SemanticOK = false
				score.Errors = append(score.Errors, "R4 "+as.Name+": "+err.Error())
			}
		}
		if as.R5 != "" {
			// Grade the converter output, not the gold R5 file.
			if err := assertJSONPath(got, as.R5, as.Want); err != nil {
				score.SemanticOK = false
				score.Errors = append(score.Errors, "R5 "+as.Name+": "+err.Error())
			}
		}
	}
	// Grade detected converter loss against the declared list. Do not union
	// declared flags into the detected list first — that check cannot fail.
	if !containsAll(loss, pair.InformationLoss) {
		score.SemanticOK = false
		score.Errors = append(score.Errors, "missing declared information-loss flags")
	}
	if pair.Spec != "" && !goldHasAuthoredSource(pair.R5, pair.Spec) {
		score.SemanticOK = false
		score.Errors = append(score.Errors, "gold R5 missing authored meta.source")
	}
	return score
}

func assertR4(env scoreEnv, resourceType string, raw json.RawMessage, expr string, want []string) (string, error) {
	envelope, err := env.codec.ParseJSON(resourceType, raw)
	if err != nil {
		return engineJSONPath, assertJSONPath(raw, expr, want)
	}
	values, err := env.fp.Eval(env.ctx, expr, envelope)
	if err != nil {
		if r4CodecCannotLoad(err) {
			return engineJSONPath, assertJSONPath(raw, expr, want)
		}
		return engineFHIRPath, err
	}
	got, err := stringifyFHIR(values)
	if err != nil {
		return engineFHIRPath, err
	}
	return engineFHIRPath, compareValues(expr, got, want)
}

func r4CodecCannotLoad(err error) bool {
	if err == nil {
		return false
	}
	s := err.Error()
	return strings.Contains(s, "unknown field") || strings.Contains(s, "expected array")
}

func stringifyFHIR(values []fhirpath.Value) ([]string, error) {
	out := make([]string, 0, len(values))
	for _, v := range values {
		if s, err := v.String(); err == nil {
			out = append(out, s)
			continue
		}
		if n, err := v.Float64(); err == nil {
			if n == float64(int(n)) {
				out = append(out, strconv.Itoa(int(n)))
			} else {
				out = append(out, strconv.FormatFloat(n, 'g', -1, 64))
			}
			continue
		}
		if b, err := v.Bool(); err == nil {
			out = append(out, strconv.FormatBool(b))
			continue
		}
		if v.Raw() == nil {
			return nil, fmt.Errorf("unstringifiable FHIRPath value type %s", v.Type())
		}
		// Proto bound codes (GenderCode, StatusCode, …) stringify as value:FEMALE.
		out = append(out, fmt.Sprint(v.Raw()))
	}
	return out, nil
}

func assertJSONPath(raw json.RawMessage, expr string, want []string) error {
	got, err := evalJSONPath(raw, expr)
	if err != nil {
		return err
	}
	return compareValues(expr, got, want)
}

func compareValues(expr string, got, want []string) error {
	if len(want) == 0 {
		if len(got) == 0 {
			return fmt.Errorf("%s: empty result", expr)
		}
		return nil
	}
	if len(got) != len(want) {
		return fmt.Errorf("%s = %v, want %v", expr, got, want)
	}
	for i := range want {
		if !equalFoldValue(got[i], want[i]) {
			return fmt.Errorf("%s[%d] = %q, want %q", expr, i, got[i], want[i])
		}
	}
	return nil
}

func equalFoldValue(got, want string) bool {
	g := strings.TrimPrefix(strings.ToLower(got), "value:")
	w := strings.ToLower(want)
	return g == w
}

func evalJSONPath(raw json.RawMessage, expr string) ([]string, error) {
	var root any
	if err := json.Unmarshal(raw, &root); err != nil {
		return nil, err
	}
	parts := strings.Split(expr, ".")
	if len(parts) > 0 {
		if obj, ok := root.(map[string]any); ok {
			if rt, _ := obj["resourceType"].(string); rt != "" && strings.EqualFold(rt, parts[0]) {
				parts = parts[1:]
			}
		}
	}
	cur := root
	for _, part := range parts {
		if part == "first()" {
			switch v := cur.(type) {
			case []any:
				if len(v) == 0 {
					return nil, fmt.Errorf("%s: empty collection", expr)
				}
				cur = v[0]
			case nil:
				return nil, fmt.Errorf("%s: empty collection", expr)
			default:
				return nil, fmt.Errorf("%s: first() requires a collection", expr)
			}
			continue
		}
		obj, ok := cur.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("%s: %q is not an object", expr, part)
		}
		cur = obj[part]
	}
	return stringifyJSON(cur), nil
}

func stringifyJSON(v any) []string {
	switch t := v.(type) {
	case nil:
		return nil
	case string:
		return []string{t}
	case float64:
		if t == float64(int(t)) {
			return []string{strconv.Itoa(int(t))}
		}
		return []string{strconv.FormatFloat(t, 'g', -1, 64)}
	case bool:
		return []string{strconv.FormatBool(t)}
	case []any:
		var out []string
		for _, item := range t {
			out = append(out, stringifyJSON(item)...)
		}
		return out
	default:
		b, err := json.Marshal(t)
		if err != nil {
			return []string{fmt.Sprint(t)}
		}
		return []string{string(b)}
	}
}

func jsonEqual(a, b []byte) (bool, error) {
	var left, right any
	if err := json.Unmarshal(a, &left); err != nil {
		return false, err
	}
	if err := json.Unmarshal(b, &right); err != nil {
		return false, err
	}
	lb, err := json.Marshal(left)
	if err != nil {
		return false, err
	}
	rb, err := json.Marshal(right)
	if err != nil {
		return false, err
	}
	return bytes.Equal(lb, rb), nil
}

func sortedCopy(in []string) []string {
	out := make([]string, 0, len(in))
	for _, item := range in {
		if item != "" {
			out = append(out, item)
		}
	}
	sort.Strings(out)
	return out
}

func containsAll(have, want []string) bool {
	set := map[string]struct{}{}
	for _, item := range have {
		set[item] = struct{}{}
	}
	for _, item := range want {
		if _, ok := set[item]; !ok {
			return false
		}
	}
	return true
}
