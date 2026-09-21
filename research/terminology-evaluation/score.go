// Command terminology-evaluation scores ConceptMap translations against a gold set.
package main

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/degoke/health-ai-stack/pkg/audit"
	"github.com/degoke/health-ai-stack/pkg/conceptmap"
	"github.com/degoke/health-ai-stack/pkg/store"
	"github.com/degoke/health-ai-stack/pkg/terminology"
	"github.com/degoke/health-ai-stack/research/internal/researchutil"
)

//go:embed gold/conceptmap.json
var conceptMapJSON []byte

//go:embed gold/translations.json
var goldJSON []byte

const (
	auditActionTranslate = "terminology.translate"
	termScope            = "research"
)

// GoldFile is the expected translation catalogue.
type GoldFile struct {
	ConceptMap          string            `json:"conceptMap"`
	ConceptMapVersion   string            `json:"conceptMapVersion"`
	SourceSystem        string            `json:"sourceSystem"`
	SourceSystemVersion string            `json:"sourceSystemVersion"`
	TargetSystem        string            `json:"targetSystem"`
	Translations        []GoldTranslation `json:"translations"`
}

// GoldTranslation is one expected (source, target, class) tuple.
type GoldTranslation struct {
	Source      string `json:"source"`
	Target      string `json:"target"`
	Equivalence string `json:"equivalence"`
}

// Provenance is the terminology decision provenance model.
type Provenance struct {
	ConceptMapURL       string    `json:"conceptMapUrl"`
	ConceptMapVersion   string    `json:"conceptMapVersion"`
	SourceSystem        string    `json:"sourceSystem"`
	SourceSystemVersion string    `json:"sourceSystemVersion"`
	TargetSystem        string    `json:"targetSystem"`
	TranslatedAt        time.Time `json:"translatedAt"`
	SourceCode          string    `json:"sourceCode"`
	TargetCode          string    `json:"targetCode,omitempty"`
	Equivalence         string    `json:"equivalence"`
}

// Metrics is the quality report.
type Metrics struct {
	Track       string       `json:"track"`
	Gold        int          `json:"gold"`
	Exact       int          `json:"exact"`
	Narrow      int          `json:"narrow"`
	Broad       int          `json:"broad"`
	Unmatched   int          `json:"unmatched"`
	TruePos     int          `json:"truePositives"`
	FalsePos    int          `json:"falsePositives"`
	FalseNeg    int          `json:"falseNegatives"`
	Precision   float64      `json:"precision"`
	Recall      float64      `json:"recall"`
	F1          float64      `json:"f1"`
	Provenance  []Provenance `json:"provenance"`
	AuditEvents int          `json:"auditEvents"`
}

func main() {
	report, err := Evaluate(context.Background())
	if err != nil {
		fmt.Fprintf(os.Stderr, "terminology-evaluation: %v\n", err)
		os.Exit(1)
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(report); err != nil {
		fmt.Fprintf(os.Stderr, "terminology-evaluation: %v\n", err)
		os.Exit(1)
	}
	if report.Gold < 1 || report.F1 < 1 {
		os.Exit(1)
	}
}

// Evaluate scores the pipeline against the gold set and emits audit events.
func Evaluate(ctx context.Context) (*Metrics, error) {
	var gold GoldFile
	if err := json.Unmarshal(goldJSON, &gold); err != nil {
		return nil, err
	}
	cmap, err := conceptmap.ParseMap(conceptMapJSON)
	if err != nil {
		return nil, err
	}
	mem := terminology.NewMemoryStore()
	if err := mem.PutResource(ctx, store.TerminologyResourceRecord{
		ScopeID:      termScope,
		ResourceType: "ConceptMap",
		ResourceID:   "haistack-vitals-to-loinc",
		CanonicalURL: gold.ConceptMap,
		Version:      gold.ConceptMapVersion,
		Status:       "active",
		ResourceJSON: conceptMapJSON,
	}); err != nil {
		return nil, err
	}
	svc := terminology.NewLocalService(mem, termScope)
	auditStore := audit.NewMemoryStore()
	logger := &audit.StoreAdapter{Store: auditStore, Now: researchutil.FixedTime}

	metrics := &Metrics{Track: "D", Gold: len(gold.Translations)}
	now := researchutil.FixedTime()

	for _, g := range gold.Translations {
		codings, err := svc.Translate(ctx, terminology.ConceptMapTranslateRequest{
			URL:          gold.ConceptMap,
			Version:      gold.ConceptMapVersion,
			TargetSystem: gold.TargetSystem,
			Coding:       terminology.Coding{System: gold.SourceSystem, Code: g.Source},
		})
		gotCode := ""
		if err == nil && len(codings) > 0 {
			gotCode = codings[0].Code
		}
		gotClass := classify(cmap, g.Source, gotCode, err)
		scoreTranslation(metrics, g, gotCode, gotClass)

		prov := Provenance{
			ConceptMapURL:       gold.ConceptMap,
			ConceptMapVersion:   gold.ConceptMapVersion,
			SourceSystem:        gold.SourceSystem,
			SourceSystemVersion: gold.SourceSystemVersion,
			TargetSystem:        gold.TargetSystem,
			TranslatedAt:        now,
			SourceCode:          g.Source,
			TargetCode:          gotCode,
			Equivalence:         gotClass,
		}
		metrics.Provenance = append(metrics.Provenance, prov)
		if err := logger.Log(ctx, audit.Event{
			ID:           "term-" + g.Source,
			Timestamp:    now,
			Actor:        "research-terminology",
			Action:       auditActionTranslate,
			Outcome:      outcomeFor(g, gotCode),
			ResourceType: "ConceptMap",
			ResourceID:   "haistack-vitals-to-loinc",
			Details: map[string]string{
				"conceptMap":          gold.ConceptMap,
				"conceptMapVersion":   gold.ConceptMapVersion,
				"sourceSystem":        gold.SourceSystem,
				"sourceSystemVersion": gold.SourceSystemVersion,
				"targetSystem":        gold.TargetSystem,
				"sourceCode":          g.Source,
				"targetCode":          gotCode,
				"equivalence":         gotClass,
			},
		}); err != nil {
			return nil, err
		}
	}

	events, err := auditStore.List(ctx, store.AuditQuery{Action: auditActionTranslate})
	if err != nil {
		return nil, err
	}
	metrics.AuditEvents = len(events)
	metrics.Precision, metrics.Recall, metrics.F1 = prf(metrics.TruePos, metrics.FalsePos, metrics.FalseNeg)
	return metrics, nil
}

func scoreTranslation(metrics *Metrics, g GoldTranslation, gotCode, gotClass string) {
	switch g.Equivalence {
	case "unmatched":
		metrics.Unmatched++
		if gotClass == "unmatched" && gotCode == "" {
			metrics.TruePos++
		} else {
			metrics.FalsePos++
		}
	default:
		match := gotCode == g.Target && gotClass == g.Equivalence
		if match {
			metrics.TruePos++
			switch g.Equivalence {
			case "exact":
				metrics.Exact++
			case "narrow":
				metrics.Narrow++
			case "broad":
				metrics.Broad++
			}
			return
		}
		if gotCode == "" {
			metrics.FalseNeg++
			return
		}
		metrics.FalsePos++
	}
}

func classify(m conceptmap.Map, source, got string, err error) string {
	if err != nil || got == "" {
		return "unmatched"
	}
	for _, group := range m.Group {
		for _, el := range group.Element {
			if el.Code != source {
				continue
			}
			if el.NoMap {
				return "unmatched"
			}
			for _, t := range el.Target {
				if t.Code != got {
					continue
				}
				return equivalenceClass(t.Equivalence)
			}
		}
	}
	return "unmatched"
}

func equivalenceClass(eq string) string {
	switch eq {
	case "equivalent", "equal":
		return "exact"
	case "narrower", "specializes", "source-is-broader-than-target":
		return "narrow"
	case "wider", "subsumes", "source-is-narrower-than-target":
		return "broad"
	case "unmatched":
		return "unmatched"
	default:
		return "exact"
	}
}

func outcomeFor(g GoldTranslation, got string) string {
	if g.Equivalence == "unmatched" && got == "" {
		return audit.OutcomeSuccess
	}
	if got == g.Target {
		return audit.OutcomeSuccess
	}
	return audit.OutcomeError
}

func prf(tp, fp, fn int) (precision, recall, f1 float64) {
	if tp+fp > 0 {
		precision = float64(tp) / float64(tp+fp)
	}
	if tp+fn > 0 {
		recall = float64(tp) / float64(tp+fn)
	}
	if precision+recall > 0 {
		f1 = 2 * precision * recall / (precision + recall)
	}
	return precision, recall, f1
}
