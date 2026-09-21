package cql

import (
	"context"
	"time"

	"github.com/degoke/health-ai-stack/pkg/fhirpath"
	"github.com/degoke/health-ai-stack/pkg/types"
)

// Engine compiles and evaluates CQL 1.5 libraries and Measure reports.
type Engine struct {
	fhirpath         fhirpath.Engine
	retriever        Retriever
	terminology      fhirpath.TerminologyValidator
	libraries        LibraryResolver
	now              func() time.Time
	maxExpressionLen int
}

// Config configures a CQL engine.
type Config struct {
	// FHIRPath is optional. When set, resource-root member access is evaluated
	// with FHIRPath first; JSON navigation remains the fallback. NewEngine does
	// not construct a FHIRPath engine when this is nil.
	FHIRPath fhirpath.Engine
	// Retriever loads clinical resources for CQL retrieve expressions.
	Retriever Retriever
	// Terminology is optional. When set, retrieve and `in` valueset filters use
	// MemberOf; otherwise matching is structured Coding/CodeableConcept equality.
	Terminology fhirpath.TerminologyValidator
	// Libraries resolves included CQL libraries (except builtin FHIRHelpers).
	Libraries LibraryResolver
	// Now overrides the evaluation clock (defaults to time.Now UTC).
	Now func() time.Time
	// MaxExpressionLen caps CQL source length (default 65536).
	MaxExpressionLen int
}

const DefaultMaxExpressionLen = 65536

// Library is a compiled CQL library.
type Library struct {
	Name        string
	Version     string
	Using       string
	Context     string
	Includes    []Include
	Defines     []Define
	Functions   []Function
	Parameters  []Parameter
	CodeSystems []CodeSystem
	ValueSets   []ValueSet
	Codes       []Code
	Concepts    []Concept
	Source      string
	URL         string
}

// Function is a named CQL function (define function / define fluent function).
type Function struct {
	Name   string
	Access string
	Fluent bool
	Params []FunctionParam
	Body   Node
	Source string
}

// FunctionParam is a CQL function operand.
type FunctionParam struct {
	Name string
	Type string
}

// Parameter is a CQL parameter declaration with an optional default.
type Parameter struct {
	Name    string
	Type    string
	Default Node
}

// Quantity is a CQL Quantity value (5 'mg', 1 year).
type Quantity struct {
	Value float64
	Unit  string
}

// Ratio is a CQL Ratio value (numerator / denominator quantities).
type Ratio struct {
	Numerator   Quantity
	Denominator Quantity
}

// Interval is a CQL Interval value.
type Interval struct {
	Low, High             any
	LowClosed, HighClosed bool
}

// CodeSystem is a CQL codesystem declaration.
type CodeSystem struct {
	Name string
	URL  string
}

// ValueSet is a CQL valueset declaration.
type ValueSet struct {
	Name string
	URL  string
}

// Code is a CQL code declaration.
type Code struct {
	Name    string
	Code    string
	System  string
	Display string
}

// Concept is a CQL concept declaration.
type Concept struct {
	Name    string
	Codes   []string
	Display string
}

// Include records an included CQL library. FHIRHelpers is provided as a builtin.
type Include struct {
	Name    string
	Version string
	Called  string
}

// Define is a named CQL expression.
type Define struct {
	Name       string
	Access     string
	Expression Node
	Source     string
}

// EvalContext is the evaluation environment for a CQL expression.
type EvalContext struct {
	Patient     any
	Parameters  map[string]any
	Libraries   []*Library
	LibraryRefs []string
	Contained   []map[string]any
	Language    string
	Now         time.Time
	Retriever   Retriever
}

// Retriever executes CQL retrieve ([Observation], …) against a data source.
type Retriever interface {
	Retrieve(ctx context.Context, req RetrieveRequest, patient any) ([]any, error)
}

// RetrieveRequest names a FHIR resource type and optional terminology filter.
type RetrieveRequest struct {
	ResourceType string
	// Terminology is the retrieve filter as written (valueset name, code name, or literal).
	Terminology string
	// Comparator is in, =, or ~ when the retrieve used `code in` / `code =` / `code ~`.
	Comparator string
	// CodePath is the retrieve property when present (`category in`, `code =`, …).
	CodePath string
	// ValueSetURL is set when Terminology names a declared valueset.
	ValueSetURL string
	// System and Code are set when Terminology names a declared code or system|code literal.
	System string
	Code   string
}

// LibraryResolver loads a CQL library by canonical URL (url or url|version).
type LibraryResolver interface {
	Resolve(ctx context.Context, canonical string) (*Library, error)
}

// Request is passed through sdc.CQLProvider.EvaluateCQL as the input value
// when the SDC adapter has questionnaire or language metadata.
type Request struct {
	Language    string
	Name        string
	Patient     any
	Parameters  map[string]any
	Libraries   []*Library
	LibraryRefs []string
	Contained   []map[string]any
	Input       any
}

// StaticLibraries resolves libraries from an in-memory canonical table.
type StaticLibraries map[string]*Library

func (s StaticLibraries) Resolve(_ context.Context, canonical string) (*Library, error) {
	if s == nil {
		return nil, ErrLibraryNotFound
	}
	if lib, ok := s[canonical]; ok && lib != nil {
		return lib, nil
	}
	url, version := splitCanonical(canonical)
	var matches []*Library
	for key, lib := range s {
		if lib == nil {
			continue
		}
		if key == url || lib.URL == url || lib.Name == url {
			if version == "" || lib.Version == version || key == canonical {
				matches = append(matches, lib)
			}
		}
	}
	if len(matches) == 1 {
		return matches[0], nil
	}
	if len(matches) > 1 {
		return nil, errf("%w: ambiguous CQL library canonical %s", ErrLibraryNotFound, canonical)
	}
	return nil, errf("%w: %s", ErrLibraryNotFound, canonical)
}

// EnvelopeLibrary parses a FHIR Library resource envelope into CQL source.
// When the Library only has ELM, CQL is recovered from an embedded library
// string or annotation when present; otherwise ErrUnsupported is returned.
// Use Engine.CompileLibrary to evaluate ELM-only Libraries.
func EnvelopeLibrary(env *types.ResourceEnvelope) (source string, url, name, version string, err error) {
	return parseLibraryEnvelope(env)
}
