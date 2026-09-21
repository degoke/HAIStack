package terminologyeval

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/degoke/health-ai-stack/pkg/audit"
	"github.com/degoke/health-ai-stack/pkg/conceptmap"
	"github.com/degoke/health-ai-stack/pkg/store"
	"github.com/degoke/health-ai-stack/pkg/terminology"
)

// Case is one gold translation expectation.
type Case struct {
	Code   string `json:"code"`
	Class  string `json:"class"`
	Target string `json:"target,omitempty"`
}

// GoldFile is the published case list.
type GoldFile struct {
	ConceptMapURL       string `json:"conceptMapUrl"`
	ConceptMapVersion   string `json:"conceptMapVersion"`
	SourceSystem        string `json:"sourceSystem"`
	SourceSystemVersion string `json:"sourceSystemVersion"`
	Cases               []Case `json:"cases"`
}

// CaseResult is one scored translation.
type CaseResult struct {
	Code       string `json:"code"`
	WantClass  string `json:"wantClass"`
	GotClass   string `json:"gotClass"`
	Target     string `json:"target,omitempty"`
	Pass       bool   `json:"pass"`
	Provenance bool   `json:"provenanceComplete"`
}

// Metrics are ConceptMap quality measures.
type Metrics struct {
	Exact      int          `json:"exact"`
	Narrow     int          `json:"narrow"`
	Broad      int          `json:"broad"`
	Unmatched  int          `json:"unmatched"`
	Passed     int          `json:"passed"`
	Failed     int          `json:"failed"`
	Precision  float64      `json:"precision"`
	Provenance float64      `json:"provenanceCompleteness"`
	Results    []CaseResult `json:"results"`
}

// Evaluate loads the gold ConceptMap into pkg/terminology, translates each
// case, scores equivalence classes, and emits terminology.translate audit events.
func Evaluate(ctx context.Context, mapPath, casesPath string, now time.Time) (Metrics, error) {
	mapJSON, err := os.ReadFile(mapPath)
	if err != nil {
		return Metrics{}, err
	}
	caseJSON, err := os.ReadFile(casesPath)
	if err != nil {
		return Metrics{}, err
	}
	var gold GoldFile
	if err := json.Unmarshal(caseJSON, &gold); err != nil {
		return Metrics{}, err
	}
	if len(gold.Cases) == 0 {
		return Metrics{}, fmt.Errorf("terminology-evaluation: no gold cases")
	}

	mem := terminology.NewMemoryStore()
	if err := mem.PutResource(ctx, store.TerminologyResourceRecord{
		ScopeID:      "research",
		ResourceType: "ConceptMap",
		ResourceID:   "lab-to-panel",
		CanonicalURL: gold.ConceptMapURL,
		Version:      gold.ConceptMapVersion,
		Status:       "active",
		ResourceJSON: mapJSON,
	}); err != nil {
		return Metrics{}, err
	}
	svc := terminology.NewLocalService(mem, "research")

	auditStore := audit.NewMemoryStore()
	logger := &audit.StoreAdapter{Store: auditStore, Now: func() time.Time { return now }}
	cmap, err := conceptmap.ParseMap(mapJSON)
	if err != nil {
		return Metrics{}, err
	}

	var metrics Metrics
	var complete int
	for _, c := range gold.Cases {
		gotClass, target, transErr := translateClass(ctx, svc, gold, cmap, c.Code)
		pass := gotClass == c.Class && (c.Target == "" || c.Target == target)
		prov := gold.ConceptMapURL != "" && gold.ConceptMapVersion != "" && gold.SourceSystemVersion != "" && !now.IsZero()
		if prov {
			complete++
		}
		if pass {
			metrics.Passed++
		} else {
			metrics.Failed++
		}
		switch c.Class {
		case "exact":
			metrics.Exact++
		case "narrow":
			metrics.Narrow++
		case "broad":
			metrics.Broad++
		default:
			metrics.Unmatched++
		}
		outcome := audit.OutcomeSuccess
		if transErr != nil {
			outcome = audit.OutcomeError
		}
		_ = audit.LogTerminologyTranslate(ctx, logger, audit.TerminologyTranslateEvent{
			Actor:        "research-terminology",
			Tenant:       "research",
			Outcome:      outcome,
			MapURL:       gold.ConceptMapURL,
			MapVersion:   gold.ConceptMapVersion,
			SourceSystem: gold.SourceSystem,
			SourceCode:   c.Code,
			TargetCode:   target,
			Timestamp:    now,
			Details: map[string]string{
				"sourceSystemVersion": gold.SourceSystemVersion,
				"equivalenceClass":    gotClass,
				"wantClass":           c.Class,
			},
		})
		metrics.Results = append(metrics.Results, CaseResult{
			Code:       c.Code,
			WantClass:  c.Class,
			GotClass:   gotClass,
			Target:     target,
			Pass:       pass,
			Provenance: prov,
		})
	}
	if metrics.Passed+metrics.Failed > 0 {
		metrics.Precision = float64(metrics.Passed) / float64(metrics.Passed+metrics.Failed)
	}
	if len(gold.Cases) > 0 {
		metrics.Provenance = float64(complete) / float64(len(gold.Cases))
	}
	return metrics, nil
}

func translateClass(ctx context.Context, svc *terminology.LocalService, gold GoldFile, cmap conceptmap.Map, code string) (class, target string, err error) {
	codings, err := svc.Translate(ctx, terminology.ConceptMapTranslateRequest{
		URL:     gold.ConceptMapURL,
		Version: gold.ConceptMapVersion,
		Coding:  terminology.Coding{System: gold.SourceSystem, Code: code},
	})
	if err != nil || len(codings) == 0 {
		return "unmatched", "", err
	}
	target = codings[0].Code
	return classFromEquivalence(lookupEquivalence(cmap, code)), target, nil
}

func lookupEquivalence(m conceptmap.Map, code string) string {
	for _, group := range m.Group {
		for _, element := range group.Element {
			if element.Code != code {
				continue
			}
			if element.NoMap || len(element.Target) == 0 {
				return "unmatched"
			}
			eq := element.Target[0].Equivalence
			if eq == "" {
				eq = element.Target[0].Relationship
			}
			return eq
		}
	}
	return "unmatched"
}

func classFromEquivalence(eq string) string {
	switch eq {
	case "equivalent", "equal":
		return "exact"
	case "narrower", "specializes", "source-is-broader-than-target":
		return "narrow"
	case "wider", "subsumes", "source-is-narrower-than-target":
		return "broad"
	default:
		return "unmatched"
	}
}

// TestdataPaths returns the published gold ConceptMap and case list.
func TestdataPaths() (mapPath, casesPath string) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		return "testdata/conceptmap.json", "testdata/cases.json"
	}
	dir := filepath.Dir(file)
	return filepath.Join(dir, "testdata", "conceptmap.json"), filepath.Join(dir, "testdata", "cases.json")
}

// FixedNow is the deterministic evaluation timestamp.
func FixedNow() time.Time {
	return time.Date(2026, 9, 21, 13, 0, 0, 0, time.UTC)
}
