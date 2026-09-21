package cql

import (
	"context"
	"strings"
	"time"
)

// NewEngine constructs a CQL engine. Config.FHIRPath is stored as-is and is
// never defaulted; JSON navigation is used when it is nil.
func NewEngine(cfg Config) (*Engine, error) {
	if cfg.MaxExpressionLen <= 0 {
		cfg.MaxExpressionLen = DefaultMaxExpressionLen
	}
	now := cfg.Now
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	ucumConv := cfg.UCUM
	if ucumConv == nil {
		ucumConv = DefaultUCUMConverter()
	}
	return &Engine{
		fhirpath:         cfg.FHIRPath,
		retriever:        cfg.Retriever,
		terminology:      cfg.Terminology,
		ucumConverter:    ucumConv,
		libraries:        cfg.Libraries,
		now:              now,
		maxExpressionLen: cfg.MaxExpressionLen,
	}, nil
}

func (e *Engine) clock() time.Time {
	if e == nil || e.now == nil {
		return time.Now().UTC()
	}
	return e.now()
}

// ParseLibrary compiles CQL library source.
func (e *Engine) ParseLibrary(src string) (*Library, error) {
	if e == nil {
		return nil, ErrEngineUnavailable
	}
	if err := e.checkLen(src); err != nil {
		return nil, err
	}
	return parseLibrary(src)
}

// ParseELM compiles an application/elm+json library into the CQL evaluator AST.
func (e *Engine) ParseELM(src []byte) (*Library, error) {
	if e == nil {
		return nil, ErrEngineUnavailable
	}
	if err := e.checkLen(string(src)); err != nil {
		return nil, err
	}
	return parseELMLibrary(src)
}

// ParseExpression compiles a CQL expression (not a full library).
func (e *Engine) ParseExpression(src string) (Node, error) {
	if e == nil {
		return nil, ErrEngineUnavailable
	}
	if err := e.checkLen(src); err != nil {
		return nil, err
	}
	return parseExpression(src)
}

// Eval evaluates a CQL expression or named identifier against env.
func (e *Engine) Eval(ctx context.Context, expr string, env EvalContext) ([]any, error) {
	if e == nil {
		return nil, ErrEngineUnavailable
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	expr = strings.TrimSpace(expr)
	if expr == "" {
		return nil, ErrEmptyExpression
	}
	if err := e.checkLen(expr); err != nil {
		return nil, err
	}
	libs := append([]*Library(nil), env.Libraries...)
	if isIdentifierLanguage(env.Language) {
		name := defineNameFromExpression(expr)
		if !lookupDefineName(libs, name) {
			return nil, errf("%w: %s", ErrExpressionNotFound, name)
		}
		def, lib := findDefine(libs, name)
		return e.evalNode(ctx, def.Expression, env, withCurrent(libs, lib))
	}
	if isSimpleIdentifier(expr) && lookupDefineName(libs, identifierName(expr)) {
		name := identifierName(expr)
		def, lib := findDefine(libs, name)
		return e.evalNode(ctx, def.Expression, env, withCurrent(libs, lib))
	}
	if looksLikeLibrary(expr) {
		lib, err := parseLibrary(expr)
		if err != nil {
			return nil, err
		}
		libs = append([]*Library{lib}, libs...)
		if len(lib.Defines) == 0 {
			return nil, errf("%w: library %q has no define statements", ErrExpressionNotFound, lib.Name)
		}
		return e.evalNode(ctx, lib.Defines[len(lib.Defines)-1].Expression, env, libs)
	}
	n, err := parseExpression(expr)
	if err != nil {
		return nil, err
	}
	if ident, ok := n.(*identNode); ok && lookupDefineName(libs, ident.name) {
		def, lib := findDefine(libs, ident.name)
		return e.evalNode(ctx, def.Expression, env, withCurrent(libs, lib))
	}
	result, err := e.evalNode(ctx, n, env, libs)
	if err != nil {
		return nil, err
	}
	return result, nil
}

// EvalDefine evaluates a named expression in a compiled library.
func (e *Engine) EvalDefine(ctx context.Context, lib *Library, name string, env EvalContext) ([]any, error) {
	if e == nil {
		return nil, ErrEngineUnavailable
	}
	if lib == nil {
		return nil, errf("%w: library is nil", ErrLibraryNotFound)
	}
	env.Libraries = withCurrent(append([]*Library{lib}, env.Libraries...), lib)
	def, found := defineByName(lib, name)
	if !found {
		return nil, errf("%w: %s", ErrExpressionNotFound, name)
	}
	return e.evalNode(ctx, def.Expression, env, env.Libraries)
}

func (e *Engine) resolveIncludes(ctx context.Context, libs []*Library) ([]*Library, error) {
	out := append([]*Library(nil), libs...)
	seen := map[string]bool{}
	for i := 0; i < len(out); i++ {
		lib := out[i]
		if lib == nil {
			continue
		}
		for _, inc := range lib.Includes {
			key := includeIdentity(inc)
			if key == "" || seen[key] {
				continue
			}
			seen[key] = true
			if strings.EqualFold(inc.Name, "FHIRHelpers") {
				helpers := fhirHelpersLibrary()
				if inc.Called != "" {
					helpers.Name = inc.Called
				}
				out = append(out, helpers)
				continue
			}
			if e == nil || e.libraries == nil {
				continue
			}
			canonical := inc.Name
			if inc.Version != "" {
				canonical = inc.Name + "|" + inc.Version
			}
			resolved, err := e.libraries.Resolve(ctx, canonical)
			if err != nil {
				return nil, err
			}
			if resolved == nil {
				continue
			}
			if inc.Called != "" && resolved.Name != inc.Called {
				cp := *resolved
				cp.Name = inc.Called
				resolved = &cp
			}
			out = append(out, resolved)
		}
	}
	return out, nil
}

const fhirHelpersBuiltinURL = "urn:haistack:cql:FHIRHelpers"

func fhirHelpersLibrary() *Library {
	return &Library{Name: "FHIRHelpers", Version: "4.0.1", Context: "Unfiltered", URL: fhirHelpersBuiltinURL}
}

func isFHIRHelpersLibrary(lib *Library) bool {
	if lib == nil {
		return false
	}
	if lib.URL == fhirHelpersBuiltinURL {
		return true
	}
	return strings.EqualFold(lib.Name, "FHIRHelpers") && len(lib.Defines) == 0 && len(lib.Functions) == 0
}

func includeIdentity(inc Include) string {
	name := strings.ToLower(strings.TrimSpace(inc.Name))
	if name == "" {
		return ""
	}
	called := strings.ToLower(strings.TrimSpace(inc.Called))
	if called == "" {
		called = name
	}
	return name + "|" + strings.ToLower(strings.TrimSpace(inc.Version)) + "|" + called
}

func (e *Engine) checkLen(src string) error {
	if e.maxExpressionLen > 0 && len(src) > e.maxExpressionLen {
		return errf("CQL expression exceeds maximum length %d", e.maxExpressionLen)
	}
	return nil
}

func isIdentifierLanguage(lang string) bool {
	return strings.EqualFold(strings.TrimSpace(lang), "text/cql.identifier")
}

func looksLikeLibrary(src string) bool {
	s := strings.TrimSpace(src)
	return len(s) >= 7 && strings.EqualFold(s[:7], "library")
}

func lookupDefineName(libs []*Library, name string) bool {
	_, lib := findDefine(libs, name)
	return lib != nil
}

func findDefine(libs []*Library, name string) (Define, *Library) {
	for _, lib := range libs {
		if lib == nil {
			continue
		}
		if def, ok := defineByName(lib, name); ok {
			return def, lib
		}
	}
	return Define{}, nil
}

func withCurrent(libs []*Library, current *Library) []*Library {
	if current == nil {
		return libs
	}
	out := make([]*Library, 0, len(libs)+1)
	out = append(out, current)
	for _, lib := range libs {
		if lib != current {
			out = append(out, lib)
		}
	}
	return out
}

func requiresPatient(env EvalContext, libs []*Library) bool {
	if strings.EqualFold(envLanguageContext(env, libs), "Patient") {
		return true
	}
	return false
}

func envLanguageContext(env EvalContext, libs []*Library) string {
	for _, lib := range libs {
		if lib != nil && lib.Context != "" {
			return lib.Context
		}
	}
	return "Patient"
}
