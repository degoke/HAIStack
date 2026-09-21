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
var goldConceptMapJSON []byte

//go:embed gold/translations.json
var goldJSON []byte

//go:embed fixture/defective-conceptmap.json
var fixtureConceptMapJSON []byte

const (
	auditActionTranslate = "terminology.translate"
	termScope            = "research"
	goldMapID            = "haistack-vitals-to-loinc"
	fixtureMapID         = "haistack-vitals-to-loinc-fixture"
)

// GoldFile is the expected translation catalogue. Tuples are independent of
// the defective fixture map (see fixture/defective-conceptmap.json).
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

// MapScore is precision/recall/F1 for one ConceptMap against the gold tuples.
type MapScore struct {
	Source     string `json:"source"`
	MapVersion string `json:"mapVersion"`
	// Exact, Narrow, Broad, and Unmatched count gold tuples that scored
	// as a true positive in that class — not how many gold tuples have
	// that label.
	Exact      int        `json:"exact"`
	Narrow     int        `json:"narrow"`
	Broad      int        `json:"broad"`
	Unmatched  int        `json:"unmatched"`
	TruePos    int        `json:"truePositives"`
	FalsePos   int        `json:"falsePositives"`
	FalseNeg   int        `json:"falseNegatives"`
	Precision  float64    `json:"precision"`
	Recall     float64    `json:"recall"`
	F1         float64    `json:"f1"`
	Mismatches []Mismatch `json:"mismatches,omitempty"`
}

// Mismatch is one gold tuple the map did not reproduce.
type Mismatch struct {
	Source     string `json:"source"`
	GoldTarget string `json:"goldTarget"`
	GoldClass  string `json:"goldClass"`
	GotTarget  string `json:"gotTarget"`
	GotClass   string `json:"gotClass"`
	Kind       string `json:"kind"` // fp | fn
}

// Metrics is the report: gold-map harness control plus defective-fixture score.
type Metrics struct {
	Track       string       `json:"track"`
	Gold        int          `json:"gold"`
	GoldMap     *MapScore    `json:"goldMap"`
	FixtureMap  *MapScore    `json:"fixtureMap"`
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
	if report.Gold < 1 || report.GoldMap == nil || report.FixtureMap == nil {
		os.Exit(1)
	}
	if !perfect(report.GoldMap, report.Gold) {
		fmt.Fprintf(os.Stderr, "terminology-evaluation: gold control failed (translator/scorer harness, f1=%v)\n", report.GoldMap.F1)
		os.Exit(1)
	}
	if perfect(report.FixtureMap, report.Gold) {
		fmt.Fprintf(os.Stderr, "terminology-evaluation: defective fixture agreed with gold; scorer cannot report F1<1\n")
		os.Exit(1)
	}
}

func perfect(s *MapScore, gold int) bool {
	return s != nil && s.TruePos == gold && s.FalsePos == 0 && s.FalseNeg == 0
}

// Evaluate scores the gold ConceptMap (translator/scorer control) and a
// planted defective fixture against the same independent translation
// catalogue. The fixture is not an independently produced map.
func Evaluate(ctx context.Context) (*Metrics, error) {
	var gold GoldFile
	if err := json.Unmarshal(goldJSON, &gold); err != nil {
		return nil, err
	}
	if err := checkGoldMetadata(gold, goldConceptMapJSON); err != nil {
		return nil, err
	}
	goldScore, _, err := scoreMap(ctx, "gold", goldMapID, goldConceptMapJSON, gold, nil)
	if err != nil {
		return nil, err
	}
	auditStore := audit.NewMemoryStore()
	logger := &audit.StoreAdapter{Store: auditStore, Now: researchutil.FixedTime}
	fixtureScore, provenance, err := scoreMap(ctx, "fixture", fixtureMapID, fixtureConceptMapJSON, gold, logger)
	if err != nil {
		return nil, err
	}
	events, err := auditStore.List(ctx, store.AuditQuery{Action: auditActionTranslate})
	if err != nil {
		return nil, err
	}
	return &Metrics{
		Track:       "D",
		Gold:        len(gold.Translations),
		GoldMap:     goldScore,
		FixtureMap:  fixtureScore,
		Provenance:  provenance,
		AuditEvents: len(events),
	}, nil
}

func checkGoldMetadata(gold GoldFile, mapJSON []byte) error {
	cmap, err := conceptmap.ParseMap(mapJSON)
	if err != nil {
		return err
	}
	if gold.ConceptMap == "" || gold.ConceptMapVersion == "" {
		return fmt.Errorf("gold translations.json must set conceptMap and conceptMapVersion")
	}
	if gold.ConceptMap != cmap.URL {
		return fmt.Errorf("gold translations conceptMap %q != gold ConceptMap url %q", gold.ConceptMap, cmap.URL)
	}
	if gold.ConceptMapVersion != cmap.Version {
		return fmt.Errorf("gold translations conceptMapVersion %q != gold ConceptMap version %q", gold.ConceptMapVersion, cmap.Version)
	}
	return nil
}

func scoreMap(ctx context.Context, source, resourceID string, mapJSON []byte, gold GoldFile, logger *audit.StoreAdapter) (*MapScore, []Provenance, error) {
	cmap, err := conceptmap.ParseMap(mapJSON)
	if err != nil {
		return nil, nil, err
	}
	mem := terminology.NewMemoryStore()
	if err := mem.PutResource(ctx, store.TerminologyResourceRecord{
		ScopeID:      termScope,
		ResourceType: "ConceptMap",
		ResourceID:   resourceID,
		CanonicalURL: cmap.URL,
		Version:      cmap.Version,
		Status:       "active",
		ResourceJSON: mapJSON,
	}); err != nil {
		return nil, nil, err
	}
	svc := terminology.NewLocalService(mem, termScope)
	metrics := &MapScore{Source: source, MapVersion: cmap.Version}
	now := researchutil.FixedTime()
	var provenance []Provenance

	for _, g := range gold.Translations {
		codings, err := svc.Translate(ctx, terminology.ConceptMapTranslateRequest{
			URL:          cmap.URL,
			Version:      cmap.Version,
			TargetSystem: gold.TargetSystem,
			Coding:       terminology.Coding{System: gold.SourceSystem, Code: g.Source},
		})
		gotCode := ""
		if err == nil && len(codings) > 0 {
			gotCode = codings[0].Code
		}
		gotClass := classify(cmap, g.Source, gotCode, err)
		scoreTranslation(metrics, g, gotCode, gotClass)

		if logger == nil {
			continue
		}
		prov := Provenance{
			ConceptMapURL:       cmap.URL,
			ConceptMapVersion:   cmap.Version,
			SourceSystem:        gold.SourceSystem,
			SourceSystemVersion: gold.SourceSystemVersion,
			TargetSystem:        gold.TargetSystem,
			TranslatedAt:        now,
			SourceCode:          g.Source,
			TargetCode:          gotCode,
			Equivalence:         gotClass,
		}
		provenance = append(provenance, prov)
		if err := logger.Log(ctx, audit.Event{
			ID:           "term-" + g.Source,
			Timestamp:    now,
			Actor:        "research-terminology",
			Action:       auditActionTranslate,
			Outcome:      outcomeFor(g, gotCode, gotClass),
			ResourceType: "ConceptMap",
			ResourceID:   resourceID,
			Details: map[string]string{
				"conceptMap":          cmap.URL,
				"conceptMapVersion":   cmap.Version,
				"sourceSystem":        gold.SourceSystem,
				"sourceSystemVersion": gold.SourceSystemVersion,
				"targetSystem":        gold.TargetSystem,
				"sourceCode":          g.Source,
				"targetCode":          gotCode,
				"equivalence":         gotClass,
			},
		}); err != nil {
			return nil, nil, err
		}
	}

	metrics.Precision, metrics.Recall, metrics.F1 = prf(metrics.TruePos, metrics.FalsePos, metrics.FalseNeg)
	return metrics, provenance, nil
}

func scoreTranslation(metrics *MapScore, g GoldTranslation, gotCode, gotClass string) {
	switch g.Equivalence {
	case "unmatched":
		if gotClass == "unmatched" && gotCode == "" {
			metrics.TruePos++
			metrics.Unmatched++
		} else {
			metrics.FalsePos++
			metrics.Mismatches = append(metrics.Mismatches, mismatch(g, gotCode, gotClass, "fp"))
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
		kind := "fp"
		if gotCode == "" {
			metrics.FalseNeg++
			kind = "fn"
		} else {
			metrics.FalsePos++
		}
		metrics.Mismatches = append(metrics.Mismatches, mismatch(g, gotCode, gotClass, kind))
	}
}

func mismatch(g GoldTranslation, gotCode, gotClass, kind string) Mismatch {
	return Mismatch{
		Source:     g.Source,
		GoldTarget: g.Target,
		GoldClass:  g.Equivalence,
		GotTarget:  gotCode,
		GotClass:   gotClass,
		Kind:       kind,
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

func outcomeFor(g GoldTranslation, got, gotClass string) string {
	if g.Equivalence == "unmatched" && got == "" && gotClass == "unmatched" {
		return audit.OutcomeSuccess
	}
	if got == g.Target && gotClass == g.Equivalence {
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
