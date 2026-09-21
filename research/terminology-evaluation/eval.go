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

// GoldFile is the published case list. ConceptMap URL/version come from the
// ConceptMap resource the translator resolves, not from this envelope.
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
type ClassScore struct {
	Precision float64 `json:"precision"`
	Recall    float64 `json:"recall"`
	Support   int     `json:"support"`
	Predicted int     `json:"predicted"`
}

// Metrics grade the translator against gold.
// Exact/Narrow/Broad/Unmatched count observed classes from $translate
// equivalence on the returned coding.
// Accuracy is the pass rate. Precision and recall are macro-averages of
// byClass (classes with predicted>0 for precision, support>0 for recall),
// which are not identical to accuracy.
// ProvenanceCompleteness is the share of translations whose audit event was
// emitted by pkg/terminology.Translate from the resolved ConceptMap.
type Metrics struct {
	Exact      int                   `json:"exact"`
	Narrow     int                   `json:"narrow"`
	Broad      int                   `json:"broad"`
	Unmatched  int                   `json:"unmatched"`
	Passed     int                   `json:"passed"`
	Failed     int                   `json:"failed"`
	Accuracy   float64               `json:"accuracy"`
	Precision  float64               `json:"precision"`
	Recall     float64               `json:"recall"`
	ByClass    map[string]ClassScore `json:"byClass,omitempty"`
	Provenance float64               `json:"provenanceCompleteness"`
	Results    []CaseResult          `json:"results"`
}

// Evaluate loads the gold ConceptMap into pkg/terminology, translates each
// case, and scores $translate output against gold. Provenance events are
// emitted inside Translate when the service is configured with audit.
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

	cmap, err := conceptmap.ParseMap(mapJSON)
	if err != nil {
		return Metrics{}, err
	}
	if cmap.URL == "" {
		return Metrics{}, fmt.Errorf("terminology-evaluation: ConceptMap missing url")
	}

	mem := terminology.NewMemoryStore()
	if err := mem.PutResource(ctx, store.TerminologyResourceRecord{
		ScopeID:      "research",
		ResourceType: "ConceptMap",
		ResourceID:   "lab-to-panel",
		CanonicalURL: cmap.URL,
		Version:      cmap.Version,
		Status:       "active",
		ResourceJSON: mapJSON,
	}); err != nil {
		return Metrics{}, err
	}

	auditStore := audit.NewMemoryStore()
	logger := &audit.StoreAdapter{Store: auditStore, Now: func() time.Time { return now }}
	svc := terminology.NewLocalService(mem, "research",
		terminology.WithTranslateAudit(logger, "research-terminology", "research", func() time.Time { return now }),
	)

	var metrics Metrics
	var complete int
	classTP := map[string]int{}
	classFP := map[string]int{}
	classSupport := map[string]int{}
	for _, c := range gold.Cases {
		gotClass, target, _ := translateClass(ctx, svc, cmap.URL, cmap.Version, gold.SourceSystem, c.Code)
		pass := gotClass == c.Class && (c.Target == "" || c.Target == target)
		if pass {
			metrics.Passed++
			classTP[gotClass]++
		} else {
			metrics.Failed++
			classFP[gotClass]++
		}
		classSupport[c.Class]++
		incrementObservedClass(&metrics, gotClass)

		events, err := logger.ListEvents(ctx, audit.Query{
			Action: audit.ActionTerminologyTranslate,
			Limit:  len(gold.Cases) + 1,
		})
		if err != nil {
			return Metrics{}, err
		}
		if len(events) == 0 {
			return Metrics{}, fmt.Errorf("terminology-evaluation: Translate did not emit audit for %s", c.Code)
		}
		prov := provenanceComplete(events[len(events)-1])
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
	metrics.Precision, metrics.Recall = macroAverage(metrics.ByClass)
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
			s.Precision = float64(tp[class]) / float64(s.Predicted)
		}
		if s.Support > 0 {
			s.Recall = float64(tp[class]) / float64(s.Support)
		}
		if s.Support > 0 || s.Predicted > 0 {
			out[class] = s
		}
	}
	return out
}

func macroAverage(byClass map[string]ClassScore) (precision, recall float64) {
	var pSum, rSum float64
	var pN, rN int
	for _, class := range []string{"exact", "narrow", "broad", "unmatched"} {
		s, ok := byClass[class]
		if !ok {
			continue
		}
		if s.Predicted > 0 {
			pSum += s.Precision
			pN++
		}
		if s.Support > 0 {
			rSum += s.Recall
			rN++
		}
	}
	if pN > 0 {
		precision = pSum / float64(pN)
	}
	if rN > 0 {
		recall = rSum / float64(rN)
	}
	return precision, recall
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
	return ev.Details["conceptMapUrl"] != "" &&
		ev.Details["conceptMapVersion"] != "" &&
		ev.Details["sourceSystemVersion"] != ""
}

func translateClass(ctx context.Context, svc *terminology.LocalService, mapURL, mapVersion, sourceSystem, code string) (class, target string, err error) {
	codings, err := svc.Translate(ctx, terminology.ConceptMapTranslateRequest{
		URL:     mapURL,
		Version: mapVersion,
		Coding:  terminology.Coding{System: sourceSystem, Code: code},
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
