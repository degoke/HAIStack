package core_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/degoke/health-ai-stack/pkg/core"
	"github.com/degoke/health-ai-stack/pkg/store"
	"github.com/degoke/health-ai-stack/pkg/types"
)

func TestVReadReturnsHistoricalVersion(t *testing.T) {
	harness := newTestHarness(t, harnessOptions{})
	ctx := context.Background()
	created, err := harness.svc.Create(ctx, patientEnvelope("pat-1", "Doe"))
	if err != nil {
		t.Fatal(err)
	}
	updated, err := harness.svc.Update(ctx, patientEnvelope("pat-1", "Smith"))
	if err != nil {
		t.Fatal(err)
	}
	got, err := harness.svc.VRead(ctx, "Patient", "pat-1", created.VersionID)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(got.JSON), `"Doe"`) {
		t.Fatalf("vread json = %s", got.JSON)
	}
	current, err := harness.svc.VRead(ctx, "Patient", "pat-1", updated.VersionID)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(current.JSON), `"Smith"`) {
		t.Fatalf("current json = %s", current.JSON)
	}
}

func TestVReadDeletedVersionIsGone(t *testing.T) {
	harness := newTestHarness(t, harnessOptions{})
	ctx := context.Background()
	created, err := harness.svc.Create(ctx, patientEnvelope("pat-1", "Doe"))
	if err != nil {
		t.Fatal(err)
	}
	if err := harness.svc.Delete(ctx, "Patient", "pat-1"); err != nil {
		t.Fatal(err)
	}
	history, err := harness.svc.History(ctx, "Patient", "pat-1")
	if err != nil {
		t.Fatal(err)
	}
	var deletedVID string
	for _, version := range history {
		if version.Action == store.VersionActionDelete {
			deletedVID = version.VersionID
		}
	}
	if deletedVID == "" {
		t.Fatal("expected delete version")
	}
	_, err = harness.svc.VRead(ctx, "Patient", "pat-1", deletedVID)
	if core.KindOf(err) != core.ErrorKindGone {
		t.Fatalf("kind = %s err = %v", core.KindOf(err), err)
	}
	if _, err := harness.svc.VRead(ctx, "Patient", "pat-1", created.VersionID); err != nil {
		t.Fatalf("historical create should still vread: %v", err)
	}
}

func TestFilterHistorySinceAndAt(t *testing.T) {
	t1 := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	t2 := time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC)
	t3 := time.Date(2024, 1, 3, 0, 0, 0, 0, time.UTC)
	versions := []store.ResourceVersion{
		{VersionID: "1", Timestamp: t1},
		{VersionID: "2", Timestamp: t2},
		{VersionID: "3", Timestamp: t3},
	}
	since := t2
	got := core.FilterHistory(versions, core.HistoryQuery{Since: &since})
	if len(got) != 2 || got[0].VersionID != "2" {
		t.Fatalf("since = %+v", got)
	}
	at := t2.Add(time.Hour)
	got = core.FilterHistory(versions, core.HistoryQuery{At: &at})
	if len(got) != 1 || got[0].VersionID != "2" {
		t.Fatalf("at = %+v", got)
	}
}

func TestEverythingIncludesPatientCompartment(t *testing.T) {
	harness := newTestHarness(t, harnessOptions{})
	ctx := context.Background()
	if _, err := harness.svc.Create(ctx, patientEnvelope("pat-1", "Doe")); err != nil {
		t.Fatal(err)
	}
	if _, err := harness.svc.Create(ctx, patientEnvelope("pat-2", "Other")); err != nil {
		t.Fatal(err)
	}
	obs := observationEnvelope("obs-1", "pat-1")
	if _, err := harness.svc.Create(ctx, obs); err != nil {
		t.Fatal(err)
	}
	other := observationEnvelope("obs-2", "pat-2")
	if _, err := harness.svc.Create(ctx, other); err != nil {
		t.Fatal(err)
	}
	got, err := harness.svc.Everything(ctx, "pat-1", core.EverythingQuery{Types: []string{"Patient", "Observation"}})
	if err != nil {
		t.Fatal(err)
	}
	ids := map[string]bool{}
	for _, env := range got {
		ids[env.ResourceType+"/"+env.ID] = true
	}
	if !ids["Patient/pat-1"] || !ids["Observation/obs-1"] {
		t.Fatalf("missing compartment resources: %v", ids)
	}
	if ids["Patient/pat-2"] || ids["Observation/obs-2"] {
		t.Fatalf("leaked other patient: %v", ids)
	}
}

func TestFHIRPatchReplaceAndDelete(t *testing.T) {
	harness := newTestHarness(t, harnessOptions{})
	ctx := context.Background()
	if _, err := harness.svc.Create(ctx, patientEnvelope("pat-1", "Doe")); err != nil {
		t.Fatal(err)
	}
	patch := []byte(`{
		"resourceType":"Parameters",
		"parameter":[{"name":"operation","part":[
			{"name":"type","valueCode":"replace"},
			{"name":"path","valueString":"Patient.gender"},
			{"name":"value","valueCode":"male"}
		]}]
	}`)
	patched, err := harness.svc.Patch(ctx, "Patient", "pat-1", patch)
	if err != nil {
		t.Fatal(err)
	}
	gender, _ := patched.StringField("gender")
	if gender != "male" {
		t.Fatalf("gender = %q", gender)
	}
	del := []byte(`{
		"resourceType":"Parameters",
		"parameter":[{"name":"operation","part":[
			{"name":"type","valueCode":"delete"},
			{"name":"path","valueString":"Patient.gender"}
		]}]
	}`)
	patched, err = harness.svc.Patch(ctx, "Patient", "pat-1", del)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := patched.StringField("gender"); ok {
		t.Fatal("expected gender removed")
	}
}

func observationEnvelope(id, patientID string) *types.ResourceEnvelope {
	payload := map[string]any{
		"resourceType": "Observation",
		"id":           id,
		"status":       "final",
		"code":         map[string]any{"text": "demo"},
		"subject":      map[string]any{"reference": "Patient/" + patientID},
	}
	data, _ := json.Marshal(payload)
	return &types.ResourceEnvelope{ResourceType: "Observation", JSON: data}
}
