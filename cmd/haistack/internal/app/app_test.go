package app_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/degoke/health-ai-stack/cmd/haistack/internal/app"
	"github.com/degoke/health-ai-stack/cmd/haistack/internal/config"
	"github.com/degoke/health-ai-stack/pkg/fhirpath"
	"github.com/verily-src/fhirpath-go/fhirpath/system"
)

func TestParseSearchParams(t *testing.T) {
	t.Parallel()
	params, err := app.ParseSearchParams([]string{"name=Smith", "family=Doe"})
	if err != nil {
		t.Fatalf("ParseSearchParams: %v", err)
	}
	if len(params["name"]) != 1 || params["name"][0] != "Smith" {
		t.Fatalf("name = %#v", params["name"])
	}
	if len(params["family"]) != 1 || params["family"][0] != "Doe" {
		t.Fatalf("family = %#v", params["family"])
	}
}

func TestParseSearchParamsRejectsInvalid(t *testing.T) {
	t.Parallel()
	if _, err := app.ParseSearchParams([]string{"invalid"}); err == nil {
		t.Fatal("expected error for missing =")
	}
}

func TestFHIRPathValuesJSONScalarAndCollection(t *testing.T) {
	t.Parallel()
	scalar, err := app.FHIRPathValuesJSON([]fhirpath.Value{
		fhirpath.NewValue(system.Boolean(true)),
	})
	if err != nil {
		t.Fatalf("FHIRPathValuesJSON scalar: %v", err)
	}
	if string(scalar) != `[{"type":"Boolean","value":true}]` {
		t.Fatalf("scalar json = %s", scalar)
	}

	collection, err := app.FHIRPathValuesJSON([]fhirpath.Value{
		fhirpath.NewValue(system.String("a")),
		fhirpath.NewValue(system.String("b")),
	})
	if err != nil {
		t.Fatalf("FHIRPathValuesJSON collection: %v", err)
	}
	if string(collection) != `[{"type":"String","value":"a"},{"type":"String","value":"b"}]` {
		t.Fatalf("collection json = %s", collection)
	}
}

func TestFHIRPathValuesJSONTemporalValues(t *testing.T) {
	t.Parallel()
	data, err := app.FHIRPathValuesJSON([]fhirpath.Value{
		fhirpath.NewValue(system.MustParseDate("@2020-01-02")),
		fhirpath.NewValue(system.MustParseDateTime("@2020-01-02T03:04:05Z")),
	})
	if err != nil {
		t.Fatalf("FHIRPathValuesJSON temporal values: %v", err)
	}
	if string(data) != `[{"type":"Date","value":"2020-01-02"},{"type":"DateTime","value":"2020-01-02T03:04:05Z"}]` {
		t.Fatalf("temporal json = %s", data)
	}
}

func TestFormatSyncStatusTextEmptyState(t *testing.T) {
	t.Parallel()
	text := app.FormatSyncStatusText(&app.SyncStatusReport{
		NodeID:              "runtime-node",
		PendingRetryPush:    0,
		UnresolvedConflicts: 0,
	})
	if text == "" {
		t.Fatal("expected text output")
	}
}

func TestBuildRuntimeWithOptInModulesOnly(t *testing.T) {
	cfg := config.Defaults()
	cfg.Storage.SQLitePath = filepath.Join(t.TempDir(), "haistack.db")
	cfg.Runtime.ModulePaths = nil

	rt, err := app.BuildRuntime(context.Background(), cfg, "")
	if err != nil {
		t.Fatalf("BuildRuntime without module paths: %v", err)
	}
	if err := rt.Shutdown(context.Background()); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}
}
