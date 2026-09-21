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
	"github.com/degoke/health-ai-stack/pkg/store"
	"github.com/degoke/health-ai-stack/pkg/terminology"
)

// EvalStoreCanonical is the local store key Evaluate uses for $translate
// lookup. It is not the ConceptMap.url written into audit events.
const EvalStoreCanonical = "urn:haistack:research:eval-conceptmap"

// Case is one gold translation expectation.
type Case struct {
	Code   string `json:"code"`
	Class  string `json:"class"`
	Target string `json:"target,omitempty"`
}

// GoldFile is the published case list. It does not carry ConceptMap URL,
// version, or source-system version; those come from the ConceptMap resource
// pkg/terminology.Translate resolves.
type GoldFile struct {
	SourceSystem string `json:"sourceSystem"`
	Cases        []Case `json:"cases"`
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

// ClassScore is one-vs-rest precision/recall for an equivalence class.
// Precision is omitted when Predicted is 0; recall is omitted when Support is 0.
type ClassScore struct {
	Precision *float64 `json:"precision,omitempty"`
	Recall    *float64 `json:"recall,omitempty"`
	Support   int      `json:"support"`
	Predicted int      `json:"predicted"`
}

// Metrics grade a ConceptMap loaded into pkg/terminology against authored cases.
// Accuracy is the pass rate (class and target). ByClass is one-vs-rest on
// class labels only; a right class with a wrong target fails accuracy and
// does not count as a class false positive.
// Gold conceptmap.json vs cases.json is a consistency check (expected 1.0).
// Held-out conceptmap vs the same cases is the class-quality evaluation.
type Metrics struct {
	Exact      int                   `json:"exact"`
	Narrow     int                   `json:"narrow"`
	Broad      int                   `json:"broad"`
	Unmatched  int                   `json:"unmatched"`
	Passed     int                   `json:"passed"`
	Failed     int                   `json:"failed"`
	Accuracy   float64               `json:"accuracy"`
	ByClass    map[string]ClassScore `json:"byClass,omitempty"`
	Provenance float64               `json:"provenanceCompleteness"`
	Results    []CaseResult          `json:"results"`
}

type lastEventLogger struct {
	inner audit.Logger
	last  audit.Event
	n     int
}

func (l *lastEventLogger) Log(ctx context.Context, event audit.Event) error {
	if err := l.inner.Log(ctx, event); err != nil {
		return err
	}
	l.last = event
	l.n++
	return nil
}

// Evaluate stores the ConceptMap JSON under EvalStoreCanonical (not the
// resource url), calls $translate with that store key, and scores output
// against authored cases. Audit url/version come from the ConceptMap body.
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
		ResourceID:   "eval-map",
		CanonicalURL: EvalStoreCanonical,
		Status:       "active",
		ResourceJSON: mapJSON,
	}); err != nil {
		return Metrics{}, err
	}

	auditStore := audit.NewMemoryStore()
	inner := &audit.StoreAdapter{Store: auditStore, Now: func() time.Time { return now }}
	logger := &lastEventLogger{inner: inner}
	svc := terminology.NewLocalService(mem, "research",
		terminology.WithTranslateAudit(logger, "research-terminology", "research", func() time.Time { return now }),
	)

	var metrics Metrics
	var complete int
	classTP := map[string]int{}
	classFP := map[string]int{}
	classSupport := map[string]int{}
	for _, c := range gold.Cases {
		before := logger.n
		gotClass, target, _ := translateClass(ctx, svc, gold.SourceSystem, c.Code)
		if logger.n != before+1 {
			return Metrics{}, fmt.Errorf("terminology-evaluation: Translate did not emit audit for %s", c.Code)
		}
		classOK := gotClass == c.Class
		targetOK := c.Target == "" || c.Target == target
		pass := classOK && targetOK
		if pass {
			metrics.Passed++
		} else {
			metrics.Failed++
		}
		if classOK {
			classTP[gotClass]++
		} else {
			classFP[gotClass]++
		}
		classSupport[c.Class]++
		incrementObservedClass(&metrics, gotClass)

		prov := provenanceComplete(logger.last)
		if prov {
			complete++
		}
		metrics.Results = append(metrics.Results, CaseResult{
			Code:       c.Code,
			WantClass:  c.Class,
			GotClass:   gotClass,
			Target:     target,
			Pass:       pass,
			Provenance: prov,
		})
	}
	total := metrics.Passed + metrics.Failed
	if total > 0 {
		metrics.Accuracy = float64(metrics.Passed) / float64(total)
	}
	metrics.ByClass = classScores(classTP, classFP, classSupport)
	if len(gold.Cases) > 0 {
		metrics.Provenance = float64(complete) / float64(len(gold.Cases))
	}
	return metrics, nil
}

func classScores(tp, fp, support map[string]int) map[string]ClassScore {
	out := map[string]ClassScore{}
	for _, class := range []string{"exact", "narrow", "broad", "unmatched"} {
		s := ClassScore{
			Support:   support[class],
			Predicted: tp[class] + fp[class],
		}
		if s.Predicted > 0 {
			p := float64(tp[class]) / float64(s.Predicted)
			s.Precision = &p
		}
		if s.Support > 0 {
			r := float64(tp[class]) / float64(s.Support)
			s.Recall = &r
		}
		if s.Support > 0 || s.Predicted > 0 {
			out[class] = s
		}
	}
	return out
}

func incrementObservedClass(metrics *Metrics, gotClass string) {
	switch gotClass {
	case "exact":
		metrics.Exact++
	case "narrow":
		metrics.Narrow++
	case "broad":
		metrics.Broad++
	default:
		metrics.Unmatched++
	}
}

func provenanceComplete(ev audit.Event) bool {
	if ev.Timestamp.IsZero() {
		return false
	}
	if ev.Details["conceptMapUrl"] == "" || ev.Details["conceptMapUrl"] == EvalStoreCanonical {
		return false
	}
	return ev.Details["conceptMapVersion"] != "" &&
		ev.Details["sourceSystemVersion"] != ""
}

func translateClass(ctx context.Context, svc *terminology.LocalService, sourceSystem, code string) (class, target string, err error) {
	codings, err := svc.Translate(ctx, terminology.ConceptMapTranslateRequest{
		URL:    EvalStoreCanonical,
		Coding: terminology.Coding{System: sourceSystem, Code: code},
	})
	if err != nil || len(codings) == 0 {
		return "unmatched", "", err
	}
	target = codings[0].Code
	return classFromEquivalence(codings[0].Equivalence), target, nil
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

func testdataDir() string {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		return "testdata"
	}
	return filepath.Join(filepath.Dir(file), "testdata")
}

// TestdataPaths returns the gold ConceptMap and authored case list.
func TestdataPaths() (mapPath, casesPath string) {
	dir := testdataDir()
	return filepath.Join(dir, "conceptmap.json"), filepath.Join(dir, "cases.json")
}

// HeldOutMapPath is a ConceptMap that disagrees with authored cases.json
// (WBC is equivalent instead of wider).
func HeldOutMapPath() string {
	return filepath.Join(testdataDir(), "heldout-conceptmap.json")
}

// SourceFile is the evaluation harness source.
func SourceFile() string {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		return "eval.go"
	}
	return file
}

// FixedNow is the deterministic evaluation timestamp.
func FixedNow() time.Time {
	return time.Date(2026, 9, 21, 13, 0, 0, 0, time.UTC)
}
