package semanticconversion

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// PairScore is the per-instance conversion score.
type PairScore struct {
	ID              string   `json:"id"`
	ResourceType    string   `json:"resourceType"`
	Category        string   `json:"category"`
	StructuralOK    bool     `json:"structuralOk"`
	SemanticOK      bool     `json:"semanticOk"`
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

// ScoreCorpus converts each R4 instance and compares it to the expected R5
// payload, then evaluates FHIRPath-subset assertions against canonical JSON.
// Assertions are written as FHIRPath; evaluation uses JSON navigation so R5
// instances can be scored before a production R5 protobuf codec exists.
func ScoreCorpus(pairs []Pair) (Report, error) {
	report := Report{ByCategory: map[string]int{}}
	for _, pair := range pairs {
		score := scorePair(pair)
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

func scorePair(pair Pair) PairScore {
	score := PairScore{
		ID:           pair.ID,
		ResourceType: pair.ResourceType,
		Category:     pair.Category,
	}
	got, loss, err := ConvertR4ToR5(pair.ResourceType, pair.R4)
	if err != nil {
		score.Errors = append(score.Errors, err.Error())
		return score
	}
	score.InformationLoss = mergeLoss(pair.InformationLoss, loss)
	eq, err := jsonEqual(got, pair.R5)
	if err != nil {
		score.Errors = append(score.Errors, err.Error())
		return score
	}
	score.StructuralOK = eq
	if !eq {
		score.Errors = append(score.Errors, "converted R5 does not match expected R5")
	}

	score.SemanticOK = true
	for _, as := range pair.Assertions {
		if as.R4 != "" {
			if err := assertPath(pair.R4, as.R4, as.Want); err != nil {
				score.SemanticOK = false
				score.Errors = append(score.Errors, "R4 "+as.Name+": "+err.Error())
			}
		}
		if as.R5 != "" {
			if err := assertPath(pair.R5, as.R5, as.Want); err != nil {
				score.SemanticOK = false
				score.Errors = append(score.Errors, "R5 "+as.Name+": "+err.Error())
			}
		}
	}
	if !containsAll(score.InformationLoss, pair.InformationLoss) {
		score.SemanticOK = false
		score.Errors = append(score.Errors, "missing declared information-loss flags")
	}
	return score
}

func assertPath(raw json.RawMessage, expr string, want []string) error {
	got, err := evalJSONPath(raw, expr)
	if err != nil {
		return err
	}
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

func mergeLoss(declared, detected []string) []string {
	seen := map[string]struct{}{}
	var out []string
	for _, item := range append(append([]string{}, declared...), detected...) {
		if item == "" {
			continue
		}
		if _, ok := seen[item]; ok {
			continue
		}
		seen[item] = struct{}{}
		out = append(out, item)
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
