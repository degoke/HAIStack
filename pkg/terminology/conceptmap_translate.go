package terminology

import (
	"context"

	"github.com/degoke/health-ai-stack/pkg/conceptmap"
)

// ConceptMapTranslateRequest is the input for the terminology $translate operation.
type ConceptMapTranslateRequest struct {
	URL, Version, TargetSystem string
	Coding                     Coding
}

// Translate performs ConceptMap translation using local projections, with optional remote fallback.
func (s *LocalService) Translate(ctx context.Context, req ConceptMapTranslateRequest) ([]Coding, error) {
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
	if s != nil && s.Store != nil {
		translator.Resolver = &conceptmap.TerminologyStoreResolver{Store: s.Store, ScopeID: s.ScopeID}
	}
	if translator.Resolver == nil && translator.Remote == nil {
		return nil, conceptmap.ErrNotFound(req.URL)
	}
	codings, err := translator.Translate(ctx, conceptmap.TranslateRequest{
		MapCanonical: canonical,
		Source:       source,
		TargetSystem: req.TargetSystem,
	})
	if err != nil {
		return nil, err
	}
	out := make([]Coding, 0, len(codings))
	for _, coding := range codings {
		out = append(out, Coding{
			System:  stringValue(coding["system"]),
			Code:    stringValue(coding["code"]),
			Display: stringValue(coding["display"]),
		})
	}
	return out, nil
}

func stringValue(value any) string {
	if value == nil {
		return ""
	}
	if s, ok := value.(string); ok {
		return s
	}
	return ""
}
