package terminologyeval

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
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

// ClassScore is one-vs-rest precision/recall for an equivalence class.
type ClassScore struct {
	Precision float64 `json:"precision"`
	Recall    float64 `json:"recall"`
	Support   int     `json:"support"`
}

// Metrics grade the translator against gold.
// Exact/Narrow/Broad/Unmatched count observed classes from $translate
// equivalence on the returned coding (not a second ConceptMap lookup).
// Precision and recall are micro-averaged multi-class: a wrong label is
// both FP (predicted class) and FN (gold class). ByClass is one-vs-rest.
// ProvenanceCompleteness is the share of translations whose emitted audit
// event records ConceptMap URL+version, source CodeSystem version, and
// timestamp taken from the ConceptMap the translator resolved — not from
// cases.json.
type Metrics struct {
	Exact      int                   `json:"exact"`
	Narrow     int                   `json:"narrow"`
	Broad      int                   `json:"broad"`
	Unmatched  int                   `json:"unmatched"`
	Passed     int                   `json:"passed"`
	Failed     int                   `json:"failed"`
	Precision  float64               `json:"precision"`
	Recall     float64               `json:"recall"`
	ByClass    map[string]ClassScore `json:"byClass,omitempty"`
	Provenance float64               `json:"provenanceCompleteness"`
	Results    []CaseResult          `json:"results"`
}

// Evaluate loads the gold ConceptMap into pkg/terminology, translates each
// case, scores the translator's equivalence class against gold, and emits
// terminology.translate audit events. Audit emit failures abort the run.
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
	mapURL := cmap.URL
	if mapURL == "" {
		mapURL = gold.ConceptMapURL
	}
	mapVersion := cmap.Version
	if mapVersion == "" {
		mapVersion = gold.ConceptMapVersion
	}

	mem := terminology.NewMemoryStore()
	if err := mem.PutResource(ctx, store.TerminologyResourceRecord{
		ScopeID:      "research",
		ResourceType: "ConceptMap",
		ResourceID:   "lab-to-panel",
		CanonicalURL: mapURL,
		Version:      mapVersion,
		Status:       "active",
		ResourceJSON: mapJSON,
	}); err != nil {
		return Metrics{}, err
	}
	svc := terminology.NewLocalService(mem, "research")

	auditStore := audit.NewMemoryStore()
	logger := &audit.StoreAdapter{Store: auditStore, Now: func() time.Time { return now }}

	var metrics Metrics
	var complete int
	var tp, fp, fn int
	classTP := map[string]int{}
	classFP := map[string]int{}
	classFN := map[string]int{}
	classSupport := map[string]int{}
	for _, c := range gold.Cases {
		gotClass, target, transErr := translateClass(ctx, svc, mapURL, mapVersion, gold.SourceSystem, c.Code)
		pass := gotClass == c.Class && (c.Target == "" || c.Target == target)
		if pass {
			metrics.Passed++
			tp++
			classTP[gotClass]++
		} else {
			metrics.Failed++
			fp++
			fn++
			classFP[gotClass]++
			classFN[c.Class]++
		}
		classSupport[c.Class]++
		incrementObservedClass(&metrics, gotClass)

		outcome := audit.OutcomeSuccess
		if transErr != nil {
			outcome = audit.OutcomeError
		}
		details := map[string]string{
			"equivalenceClass": gotClass,
			"wantClass":        c.Class,
		}
		if srcVer := versionFromCanonical(cmap.SourceURI); srcVer != "" {
			details["sourceSystemVersion"] = srcVer
		}
		if err := audit.LogTerminologyTranslate(ctx, logger, audit.TerminologyTranslateEvent{
			Actor:        "research-terminology",
			Tenant:       "research",
			Outcome:      outcome,
			MapURL:       cmap.URL,
			MapVersion:   cmap.Version,
			SourceSystem: firstNonEmpty(groupSource(cmap), gold.SourceSystem),
			SourceCode:   c.Code,
			TargetCode:   target,
			Timestamp:    now,
			Details:      details,
		}); err != nil {
			return Metrics{}, fmt.Errorf("terminology-evaluation: audit %s: %w", c.Code, err)
		}
		events, err := logger.ListEvents(ctx, audit.Query{
			Action: audit.ActionTerminologyTranslate,
			Limit:  len(gold.Cases) + 1,
		})
		if err != nil {
			return Metrics{}, err
		}
		if len(events) == 0 {
			return Metrics{}, fmt.Errorf("terminology-evaluation: no audit event for %s", c.Code)
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
	if tp+fp > 0 {
		metrics.Precision = float64(tp) / float64(tp+fp)
	}
	if tp+fn > 0 {
		metrics.Recall = float64(tp) / float64(tp+fn)
	}
	metrics.ByClass = classScores(classTP, classFP, classFN, classSupport)
	if len(gold.Cases) > 0 {
		metrics.Provenance = float64(complete) / float64(len(gold.Cases))
	}
	return metrics, nil
}

func classScores(tp, fp, fn, support map[string]int) map[string]ClassScore {
	out := map[string]ClassScore{}
	for _, class := range []string{"exact", "narrow", "broad", "unmatched"} {
		s := ClassScore{Support: support[class]}
		if t, f := tp[class], fp[class]; t+f > 0 {
			s.Precision = float64(t) / float64(t+f)
		}
		if t, n := tp[class], fn[class]; t+n > 0 {
			s.Recall = float64(t) / float64(t+n)
		}
		if s.Support > 0 || tp[class]+fp[class]+fn[class] > 0 {
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

func versionFromCanonical(canonical string) string {
	_, ver, ok := strings.Cut(canonical, "|")
	if !ok {
		return ""
	}
	return ver
}

func groupSource(m conceptmap.Map) string {
	if len(m.Group) == 0 {
		return ""
	}
	return m.Group[0].Source
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
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
