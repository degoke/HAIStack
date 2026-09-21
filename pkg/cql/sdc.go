package cql

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/degoke/health-ai-stack/pkg/sdc"
	"github.com/degoke/health-ai-stack/pkg/types"
)

// Provider implements sdc.CQLProvider.
type Provider struct {
	Engine    *Engine
	Libraries LibraryResolver
	Retriever Retriever
}

// NewProvider constructs an SDC CQL adapter.
func NewProvider(engine *Engine, libs LibraryResolver) Provider {
	return Provider{Engine: engine, Libraries: libs}
}

var _ sdc.CQLProvider = Provider{}

// EvaluateCQL evaluates a CQL expression or named library define.
func (p Provider) EvaluateCQL(ctx context.Context, expr string, input any) ([]any, error) {
	if p.Engine == nil {
		return nil, ErrEngineUnavailable
	}
	env, err := evalContextFromInput(input)
	if err != nil {
		return nil, err
	}
	if env.Retriever == nil {
		env.Retriever = p.Retriever
	}
	libs, err := p.loadLibraries(ctx, env)
	if err != nil {
		return nil, err
	}
	env.Libraries = append(env.Libraries, libs...)
	if env.Patient == nil && requiresPatient(env, env.Libraries) && expressionNeedsPatient(expr, env.Language, env.Libraries) {
		return nil, ErrMissingContext
	}
	return p.Engine.Eval(ctx, expr, env)
}

func expressionNeedsPatient(expr, language string, libs []*Library) bool {
	if isIdentifierLanguage(language) {
		for _, lib := range libs {
			if lib != nil && strings.EqualFold(lib.Context, "Patient") {
				return true
			}
		}
	}
	lx := newLexer(expr)
	for {
		t := lx.next()
		if t.kind == tEOF {
			return false
		}
		if t.kind != tIdent && t.kind != tQuotedIdent {
			continue
		}
		switch strings.ToLower(t.text) {
		case "patient", "unfiltered", "ageinyears", "ageinyearsat":
			return true
		}
	}
}

func (p Provider) loadLibraries(ctx context.Context, env EvalContext) ([]*Library, error) {
	var out []*Library
	seen := map[string]bool{}
	add := func(lib *Library) {
		if lib == nil {
			return
		}
		key := lib.URL
		if key == "" {
			key = lib.Name + "|" + lib.Version
		}
		if key == "" || seen[key] {
			return
		}
		seen[key] = true
		out = append(out, lib)
	}
	for _, lib := range env.Libraries {
		add(lib)
	}
	for _, raw := range env.Contained {
		src, url, name, version, err := parseLibraryJSON(mustMarshal(raw))
		if err != nil {
			continue
		}
		lib, err := compileLibrarySource(p.Engine, src, url, name, version)
		if err != nil {
			return nil, err
		}
		add(lib)
	}
	for _, ref := range env.LibraryRefs {
		if ref == "" {
			continue
		}
		if alreadyLoaded(out, ref) {
			continue
		}
		if p.Libraries == nil {
			return nil, errf("%w: %s", ErrLibraryNotFound, ref)
		}
		lib, err := p.Libraries.Resolve(ctx, ref)
		if err != nil {
			return nil, err
		}
		add(lib)
	}
	return out, nil
}

func alreadyLoaded(libs []*Library, canonical string) bool {
	for _, lib := range libs {
		if matchesCanonical(lib, canonical) {
			return true
		}
	}
	return false
}

func evalContextFromInput(input any) (EvalContext, error) {
	switch x := input.(type) {
	case EvalContext:
		return x, nil
	case *EvalContext:
		if x == nil {
			return EvalContext{}, nil
		}
		return *x, nil
	case Request:
		return requestToEval(x)
	case *Request:
		if x == nil {
			return EvalContext{}, nil
		}
		return requestToEval(*x)
	case sdc.CQLEvalInput:
		env, err := evalContextFromInput(x.Input)
		if err != nil {
			return EvalContext{}, err
		}
		if x.Language != "" {
			env.Language = x.Language
		}
		return env, nil
	case sdc.ExpressionEnvironment:
		return fromSDCEnv(x), nil
	case *sdc.ExpressionEnvironment:
		if x == nil {
			return EvalContext{}, nil
		}
		return fromSDCEnv(*x), nil
	case sdc.QuestionnaireResponse:
		return evalContextFromResponse(x), nil
	case *sdc.QuestionnaireResponse:
		if x == nil {
			return EvalContext{}, nil
		}
		return evalContextFromResponse(*x), nil
	case *types.ResourceEnvelope:
		return EvalContext{Patient: x}, nil
	case types.ResourceEnvelope:
		return EvalContext{Patient: &x}, nil
	case map[string]any:
		if rt, _ := x["resourceType"].(string); rt != "" {
			return EvalContext{Patient: x}, nil
		}
		if subj, ok := x["subject"]; ok {
			return evalContextFromInput(subj)
		}
		return EvalContext{Parameters: x}, nil
	default:
		return EvalContext{Patient: input}, nil
	}
}

func requestToEval(r Request) (EvalContext, error) {
	env, err := evalContextFromInput(r.Input)
	if err != nil {
		return EvalContext{}, err
	}
	if r.Language != "" {
		env.Language = r.Language
	}
	if r.Patient != nil {
		env.Patient = r.Patient
	}
	if r.Parameters != nil {
		env.Parameters = mergeParams(env.Parameters, r.Parameters)
	}
	env.Libraries = append(env.Libraries, r.Libraries...)
	env.LibraryRefs = append(env.LibraryRefs, r.LibraryRefs...)
	env.Contained = append(env.Contained, r.Contained...)
	return env, nil
}

func fromSDCEnv(env sdc.ExpressionEnvironment) EvalContext {
	out := EvalContext{Parameters: map[string]any{}}
	for name, value := range env.Constants {
		if name == "" {
			continue
		}
		out.Parameters[name] = value
	}
	if v, ok := out.Parameters["patient"]; ok && v != nil {
		out.Patient = v
	} else if v, ok := out.Parameters["subject"]; ok && v != nil {
		out.Patient = v
	}
	if out.Patient == nil && env.Context != nil {
		out.Patient = env.Context
	}
	if q, ok := env.Questionnaire.(sdc.Questionnaire); ok {
		for _, lib := range q.CQFLibraries {
			if lib.LibraryCanonical != "" {
				out.LibraryRefs = append(out.LibraryRefs, lib.LibraryCanonical)
			}
		}
		for _, c := range q.Contained {
			if rt, _ := c["resourceType"].(string); rt == "Library" {
				out.Contained = append(out.Contained, c)
			}
		}
	}
	return out
}

func evalContextFromResponse(r sdc.QuestionnaireResponse) EvalContext {
	out := EvalContext{Parameters: map[string]any{}}
	if r.Subject == nil {
		return out
	}
	out.Parameters["subject"] = r.Subject
	if rt, _ := r.Subject["resourceType"].(string); rt != "" {
		out.Patient = r.Subject
	}
	return out
}

func mergeParams(base, overlay map[string]any) map[string]any {
	out := map[string]any{}
	for k, v := range base {
		out[k] = v
	}
	for k, v := range overlay {
		out[k] = v
	}
	return out
}

func mustMarshal(v map[string]any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		return nil
	}
	return b
}

// IsLanguage reports whether lang is a CQL expression language.
func IsLanguage(lang string) bool {
	return sdc.IsCQLLanguage(lang)
}
