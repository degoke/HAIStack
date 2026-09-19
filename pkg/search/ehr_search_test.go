package search_test

import (
	"context"
	"testing"
	"time"

	"github.com/degoke/health-ai-stack/pkg/search"
	"github.com/degoke/health-ai-stack/pkg/store"
	"github.com/degoke/health-ai-stack/pkg/types"
)

func TestParseQueryHasAndTwoHopChain(t *testing.T) {
	q, err := search.ParseQueryValues("Patient", map[string][]string{
		"_has:Observation:subject:code": {"8867-4"},
		"general-practitioner.name":     {"Smith"},
	})
	if err != nil {
		t.Fatalf("ParseQuery: %v", err)
	}
	if len(q.Has) != 1 || q.Has[0].SourceType != "Observation" || q.Has[0].RefCode != "subject" || q.Has[0].Param.Code != "code" {
		t.Fatalf("has = %#v", q.Has)
	}
	if len(q.Has[0].Param.Values) != 1 || q.Has[0].Param.Values[0].Raw != "8867-4" {
		t.Fatalf("has values = %#v", q.Has[0].Param.Values)
	}

	twoHop, err := search.ParseQueryValues("Observation", map[string][]string{
		"subject.organization.name": {"Acme"},
	})
	if err != nil {
		t.Fatalf("ParseQuery two-hop: %v", err)
	}
	if len(twoHop.Chains) != 1 || twoHop.Chains[0].RefCode != "subject" || twoHop.Chains[0].Nested == nil {
		t.Fatalf("two-hop chain = %#v", twoHop.Chains)
	}
	if twoHop.Chains[0].Nested.RefCode != "organization" || twoHop.Chains[0].Nested.Param.Code != "name" {
		t.Fatalf("nested chain = %#v", twoHop.Chains[0].Nested)
	}
}

func TestParseQueryIncludeWildcards(t *testing.T) {
	q, err := search.ParseQueryValues("Observation", map[string][]string{
		"_include":    {"Observation:*", "Observation:subject:Patient", "*:*"},
		"_revinclude": {"*:*", "Observation:*"},
	})
	if err != nil {
		t.Fatalf("ParseQuery: %v", err)
	}
	if len(q.Includes) != 3 {
		t.Fatalf("includes = %#v", q.Includes)
	}
	if q.Includes[0].ParamCode != "*" || q.Includes[1].TargetType != "Patient" || q.Includes[2].ParamCode != "*" {
		t.Fatalf("includes = %#v", q.Includes)
	}
	if len(q.RevIncludes) != 2 || q.RevIncludes[0].SourceType != "*" || q.RevIncludes[1].ParamCode != "*" {
		t.Fatalf("revincludes = %#v", q.RevIncludes)
	}

	starParam, err := search.ParseQueryValues("Patient", map[string][]string{
		"_include": {"*:general-practitioner"},
	})
	if err != nil {
		t.Fatalf("ParseQuery star source: %v", err)
	}
	if len(starParam.Includes) != 1 || starParam.Includes[0].SourceType != "Patient" || starParam.Includes[0].ParamCode != "general-practitioner" {
		t.Fatalf("star source include = %#v", starParam.Includes)
	}
}

func TestResolveQueryHasUriTwoHopAndWildcards(t *testing.T) {
	snapshot := testSnapshot(t, "Patient", "Observation", "Organization", "Questionnaire")
	reg := search.NewSnapshotRegistry(snapshot)

	hasQuery, err := search.ParseQueryValues("Patient", map[string][]string{
		"_has:Observation:subject:code": {"8867-4"},
	})
	if err != nil {
		t.Fatalf("ParseQuery: %v", err)
	}
	resolvedHas, err := search.ResolveQuery(reg, hasQuery)
	if err != nil {
		t.Fatalf("ResolveQuery _has: %v", err)
	}
	if len(resolvedHas.Has) != 1 || resolvedHas.Has[0].RefFieldKey != "reference.subject" || resolvedHas.Has[0].Param.FieldKey != "token.code" {
		t.Fatalf("resolved has = %#v", resolvedHas.Has)
	}

	chainQuery, err := search.ParseQueryValues("Observation", map[string][]string{
		"subject.organization.name": {"Acme"},
	})
	if err != nil {
		t.Fatalf("ParseQuery: %v", err)
	}
	resolvedChain, err := search.ResolveQuery(reg, chainQuery)
	if err != nil {
		t.Fatalf("ResolveQuery two-hop: %v", err)
	}
	if len(resolvedChain.Chains) != 1 || resolvedChain.Chains[0].TargetType != "Patient" || resolvedChain.Chains[0].Nested == nil {
		t.Fatalf("resolved chain = %#v", resolvedChain.Chains)
	}
	if resolvedChain.Chains[0].Nested.TargetType != "Organization" || resolvedChain.Chains[0].Nested.Param.FieldKey != "string.name" {
		t.Fatalf("resolved nested chain = %#v", resolvedChain.Chains[0].Nested)
	}

	uriQuery, err := search.ParseQueryValues("Questionnaire", map[string][]string{
		"url:below": {"http://example.org/fhir"},
	})
	if err != nil {
		t.Fatalf("ParseQuery uri: %v", err)
	}
	resolvedURI, err := search.ResolveQuery(reg, uriQuery)
	if err != nil {
		t.Fatalf("ResolveQuery uri: %v", err)
	}
	if len(resolvedURI.Params) != 1 || resolvedURI.Params[0].ParamType != "uri" || resolvedURI.Params[0].FieldKey != "uri.url" {
		t.Fatalf("resolved uri = %#v", resolvedURI.Params)
	}

	incQuery, err := search.ParseQueryValues("Observation", map[string][]string{
		"_include": {"Observation:*"},
	})
	if err != nil {
		t.Fatalf("ParseQuery include: %v", err)
	}
	resolvedInc, err := search.ResolveQuery(reg, incQuery)
	if err != nil {
		t.Fatalf("ResolveQuery include wildcard: %v", err)
	}
	if len(resolvedInc.Includes) < 2 {
		t.Fatalf("wildcard includes = %#v", resolvedInc.Includes)
	}
	codes := map[string]bool{}
	for _, inc := range resolvedInc.Includes {
		if inc.ParamCode == "*" {
			t.Fatalf("wildcard was not expanded: %#v", inc)
		}
		codes[inc.ParamCode] = true
	}
	if !codes["subject"] || !codes["encounter"] {
		t.Fatalf("expanded includes missing subject/encounter: %#v", resolvedInc.Includes)
	}

	revQuery, err := search.ParseQueryValues("Patient", map[string][]string{
		"_revinclude": {"Observation:*"},
	})
	if err != nil {
		t.Fatalf("ParseQuery revinclude: %v", err)
	}
	resolvedRev, err := search.ResolveQuery(reg, revQuery)
	if err != nil {
		t.Fatalf("ResolveQuery revinclude wildcard: %v", err)
	}
	if len(resolvedRev.RevIncludes) == 0 {
		t.Fatal("expected expanded revincludes")
	}
}

func TestStoreExecutorHasAndTwoHopChain(t *testing.T) {
	ctx := context.Background()
	backend := &memSearchBackend{
		entries: []store.SearchIndexEntry{
			{ResourceType: "Organization", ID: "org-1", Fields: map[string]string{"string.name": "Acme"}},
			{ResourceType: "Organization", ID: "org-2", Fields: map[string]string{"string.name": "Other"}},
			{ResourceType: "Patient", ID: "pat-1", Fields: map[string]string{"reference.organization": "Organization/org-1"}},
			{ResourceType: "Patient", ID: "pat-2", Fields: map[string]string{"reference.organization": "Organization/org-2"}},
			{ResourceType: "Observation", ID: "obs-1", Fields: map[string]string{"reference.subject": "Patient/pat-1", "token.code": "8867-4"}},
			{ResourceType: "Observation", ID: "obs-2", Fields: map[string]string{"reference.subject": "Patient/pat-2", "token.code": "1234-5"}},
		},
	}
	resources := newMemResourceStore()
	for _, id := range []string{"pat-1", "pat-2"} {
		_ = resources.Create(ctx, &types.ResourceEnvelope{ResourceType: "Patient", ID: id})
	}
	executor := search.NewStoreExecutor(backend, resources)

	hasPlan := &search.Plan{
		ResourceType: "Patient",
		Count:        10,
		HasPlans: []search.HasPlan{{
			SourceType:  "Observation",
			RefCode:     "subject",
			RefFieldKey: "reference.subject",
			ParamPlan: search.ParamPlan{
				Code:      "code",
				FieldKey:  "token.code",
				ParamType: "token",
				Predicates: []search.Predicate{{
					FieldKey: "token.code",
					Value:    "8867-4",
					Operator: search.OpEqual,
				}},
			},
		}},
	}
	hasResult, err := executor.Execute(ctx, hasPlan)
	if err != nil {
		t.Fatalf("Execute _has: %v", err)
	}
	if len(hasResult.IDs) != 1 || hasResult.IDs[0] != "pat-1" {
		t.Fatalf("_has ids = %v", hasResult.IDs)
	}

	chainPlan := &search.Plan{
		ResourceType: "Observation",
		Count:        10,
		ChainPlans: []search.ChainPlan{{
			RefCode:     "subject",
			RefFieldKey: "reference.subject",
			TargetType:  "Patient",
			Nested: &search.ChainPlan{
				RefCode:     "organization",
				RefFieldKey: "reference.organization",
				TargetType:  "Organization",
				ParamPlan: search.ParamPlan{
					Code:      "name",
					FieldKey:  "string.name",
					ParamType: "string",
					Predicates: []search.Predicate{{
						FieldKey: "string.name",
						Value:    "Acme",
						Operator: search.OpEqual,
					}},
				},
			},
		}},
	}
	obsStore := newMemResourceStore()
	_ = obsStore.Create(ctx, &types.ResourceEnvelope{ResourceType: "Observation", ID: "obs-1"})
	_ = obsStore.Create(ctx, &types.ResourceEnvelope{ResourceType: "Observation", ID: "obs-2"})
	chainExec := search.NewStoreExecutor(backend, obsStore)
	chainResult, err := chainExec.Execute(ctx, chainPlan)
	if err != nil {
		t.Fatalf("Execute two-hop: %v", err)
	}
	if len(chainResult.IDs) != 1 || chainResult.IDs[0] != "obs-1" {
		t.Fatalf("two-hop ids = %v", chainResult.IDs)
	}
}

func TestStoreExecutorUriBelowAndWildcardInclude(t *testing.T) {
	ctx := context.Background()
	backend := &memAdvancedSearchBackend{memSearchBackend: memSearchBackend{
		entries: []store.SearchIndexEntry{
			{ResourceType: "Questionnaire", ID: "q-1", Fields: map[string]string{"uri.url": "http://example.org/fhir/Questionnaire/q-1"}},
			{ResourceType: "Questionnaire", ID: "q-2", Fields: map[string]string{"uri.url": "http://other.org/fhir/Questionnaire/q-2"}},
			{ResourceType: "Observation", ID: "obs-1", Fields: map[string]string{"reference.subject": "Patient/pat-1", "reference.encounter": "Encounter/enc-1"}},
		},
	}}
	resources := newMemResourceStore()
	_ = resources.Create(ctx, &types.ResourceEnvelope{ResourceType: "Questionnaire", ID: "q-1"})
	_ = resources.Create(ctx, &types.ResourceEnvelope{ResourceType: "Questionnaire", ID: "q-2"})
	executor := search.NewStoreExecutor(backend, resources)

	uriPlan := &search.Plan{
		ResourceType: "Questionnaire",
		Count:        10,
		ParamPlans: []search.ParamPlan{{
			Code:      "url",
			FieldKey:  "uri.url",
			ParamType: "uri",
			Predicates: []search.Predicate{{
				FieldKey: "uri.url",
				Value:    "http://example.org/fhir",
				Operator: search.OpBelow,
			}},
		}},
	}
	uriResult, err := executor.Execute(ctx, uriPlan)
	if err != nil {
		t.Fatalf("Execute uri:below: %v", err)
	}
	if len(uriResult.IDs) != 1 || uriResult.IDs[0] != "q-1" {
		t.Fatalf("uri:below ids = %v", uriResult.IDs)
	}

	obsStore := newMemResourceStore()
	_ = obsStore.Create(ctx, &types.ResourceEnvelope{ResourceType: "Observation", ID: "obs-1"})
	incExec := search.NewStoreExecutor(backend, obsStore)
	incPlan := &search.Plan{
		ResourceType: "Observation",
		Count:        10,
		Includes: []search.IncludePlan{
			{SourceType: "Observation", ParamCode: "subject", RefFieldKey: "reference.subject"},
			{SourceType: "Observation", ParamCode: "encounter", RefFieldKey: "reference.encounter"},
		},
	}
	incResult, err := incExec.Execute(ctx, incPlan)
	if err != nil {
		t.Fatalf("Execute wildcard includes: %v", err)
	}
	if len(incResult.Included) != 2 {
		t.Fatalf("included = %#v", incResult.Included)
	}
}

func TestRegistryIndexerEmitsUriFields(t *testing.T) {
	ctx := context.Background()
	snapshot := testSnapshot(t, "Questionnaire")
	reg := search.NewSnapshotRegistry(snapshot)
	indexer, err := search.NewRegistryIndexer(search.RegistryIndexerConfig{
		Registry: reg,
		Engine:   testEngine(t),
	})
	if err != nil {
		t.Fatalf("NewRegistryIndexer: %v", err)
	}
	entries, err := indexer.Build(ctx, questionnaireResource(t, "q-1", "http://example.org/fhir/Questionnaire/q-1"))
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	values := fieldValues(entries)
	if !containsValue(values, "uri.url", "http://example.org/fhir/Questionnaire/q-1") {
		t.Fatalf("missing uri.url index: %#v", values)
	}
}

func questionnaireResource(t *testing.T, id, url string) *types.ResourceEnvelope {
	t.Helper()
	data := []byte(`{
		"resourceType":"Questionnaire",
		"id":"` + id + `",
		"status":"active",
		"url":"` + url + `"
	}`)
	codec := types.NewJSONCodec()
	env, err := codec.ParseJSON("Questionnaire", data)
	if err != nil {
		t.Fatalf("ParseJSON: %v", err)
	}
	env.LastUpdated = time.Date(2024, 6, 1, 12, 0, 0, 0, time.UTC)
	return env
}

func organizationResource(t *testing.T, id, name string) *types.ResourceEnvelope {
	t.Helper()
	data := []byte(`{
		"resourceType":"Organization",
		"id":"` + id + `",
		"name":"` + name + `"
	}`)
	codec := types.NewJSONCodec()
	env, err := codec.ParseJSON("Organization", data)
	if err != nil {
		t.Fatalf("ParseJSON: %v", err)
	}
	env.LastUpdated = time.Date(2024, 6, 1, 12, 0, 0, 0, time.UTC)
	return env
}

func patientWithOrganization(t *testing.T, id, family, orgID string) *types.ResourceEnvelope {
	t.Helper()
	data := []byte(`{
		"resourceType":"Patient",
		"id":"` + id + `",
		"name":[{"family":"` + family + `","given":["Jane"]}],
		"managingOrganization":{"reference":"Organization/` + orgID + `"}
	}`)
	codec := types.NewJSONCodec()
	env, err := codec.ParseJSON("Patient", data)
	if err != nil {
		t.Fatalf("ParseJSON: %v", err)
	}
	env.LastUpdated = time.Date(2024, 6, 1, 12, 0, 0, 0, time.UTC)
	return env
}
