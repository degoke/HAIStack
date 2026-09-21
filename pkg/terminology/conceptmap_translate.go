package terminology

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/degoke/health-ai-stack/pkg/audit"
	"github.com/degoke/health-ai-stack/pkg/conceptmap"
)

// ConceptMapTranslateRequest is the input for the terminology $translate operation.
type ConceptMapTranslateRequest struct {
	URL, Version, TargetSystem string
	Coding                     Coding
}

// Translate performs ConceptMap translation using local projections, with optional remote fallback.
// When WithTranslateAudit is configured, a terminology.translate event is emitted
// from this method using the resolved ConceptMap (not the caller’s gold file).
func (s *LocalService) Translate(ctx context.Context, req ConceptMapTranslateRequest) ([]Coding, error) {
	if s == nil {
		return nil, conceptmap.ErrNotFound(req.URL)
	}
	source := map[string]any{"code": req.Coding.Code}
	if req.Coding.System != "" {
		source["system"] = req.Coding.System
	}
	if req.Coding.Display != "" {
		source["display"] = req.Coding.Display
	}
	canonical := req.URL
	if req.Version != "" {
		canonical = req.URL + "|" + req.Version
	}
	translator := conceptmap.Translator{Remote: s.RemoteTranslate}
	if s.Store != nil {
		translator.Resolver = &conceptmap.TerminologyStoreResolver{Store: s.Store, ScopeID: s.ScopeID}
	}
	if translator.Resolver == nil && translator.Remote == nil {
		return nil, conceptmap.ErrNotFound(req.URL)
	}
	raw, resolved, err := translator.TranslateResolved(ctx, conceptmap.TranslateRequest{
		MapCanonical: canonical,
		Source:       source,
		TargetSystem: req.TargetSystem,
	})
	var out []Coding
	for _, coding := range raw {
		out = append(out, Coding{
			System:      stringValue(coding["system"]),
			Code:        stringValue(coding["code"]),
			Display:     stringValue(coding["display"]),
			Equivalence: stringValue(coding["equivalence"]),
		})
	}
	if logErr := s.emitTranslateAudit(ctx, req, resolved, out, err); logErr != nil {
		if err == nil {
			return out, logErr
		}
		return out, fmt.Errorf("%w (audit: %v)", err, logErr)
	}
	return out, err
}

func (s *LocalService) emitTranslateAudit(ctx context.Context, req ConceptMapTranslateRequest, resolved conceptmap.Map, out []Coding, transErr error) error {
	if s.translateAudit == nil {
		return nil
	}
	outcome := audit.OutcomeSuccess
	if transErr != nil {
		outcome = audit.OutcomeError
	}
	target := ""
	eq := ""
	if len(out) > 0 {
		target = out[0].Code
		eq = out[0].Equivalence
	}
	sourceSystem := resolvedGroupSource(resolved)
	if sourceSystem == "" {
		sourceSystem = req.Coding.System
	}
	details := map[string]string{}
	if eq != "" {
		details["equivalence"] = eq
	}
	if srcVer := versionFromCanonical(resolved.SourceURI); srcVer != "" {
		details["sourceSystemVersion"] = srcVer
	}
	if transErr != nil {
		details["error"] = transErr.Error()
	}
	ts := time.Now().UTC()
	if s.translateNow != nil {
		ts = s.translateNow().UTC()
	}
	// Map identity is the ConceptMap TranslateResolved returned, not the
	// caller's request URL/version (those are lookup keys only).
	return audit.LogTerminologyTranslate(ctx, s.translateAudit, audit.TerminologyTranslateEvent{
		Actor:        s.translateActor,
		Tenant:       s.translateTenant,
		Outcome:      outcome,
		MapURL:       resolved.URL,
		MapVersion:   resolved.Version,
		SourceSystem: sourceSystem,
		SourceCode:   req.Coding.Code,
		TargetCode:   target,
		Timestamp:    ts,
		Details:      details,
	})
}

func resolvedGroupSource(m conceptmap.Map) string {
	if len(m.Group) == 0 {
		return ""
	}
	return m.Group[0].Source
}

func versionFromCanonical(canonical string) string {
	_, ver, ok := strings.Cut(canonical, "|")
	if !ok {
		return ""
	}
	return ver
}
