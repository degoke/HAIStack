package sqlite_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/degoke/health-ai-stack/pkg/sqlite"
	"github.com/degoke/health-ai-stack/pkg/store"
	"github.com/degoke/health-ai-stack/pkg/terminology"
)

func TestTerminologyInstallStoreListInstalledFiltersByURLAndVersion(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dir := t.TempDir()
	db, err := sqlite.OpenAndMigrate(ctx, filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()

	installs := db.TerminologyInstallStore("tenant-a")
	now := time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC)
	for _, record := range []store.TerminologyInstallRecord{
		{
			PackName: "pack-a", PackVersion: "1.0", ResourceType: "ValueSet",
			CanonicalURL: "urn:vs1", Version: "1", Enabled: true, InstalledAt: now,
		},
		{
			PackName: "pack-a", PackVersion: "1.0", ResourceType: "ValueSet",
			CanonicalURL: "urn:vs2", Version: "1", Enabled: true, InstalledAt: now,
		},
	} {
		if err := installs.UpsertInstall(ctx, record); err != nil {
			t.Fatal(err)
		}
	}

	rows, err := installs.ListInstalled(ctx, store.TerminologyInstallFilter{
		ResourceType: "ValueSet", CanonicalURL: "urn:vs2", Version: "1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].CanonicalURL != "urn:vs2" {
		t.Fatalf("rows=%+v", rows)
	}
}

func TestEnsureInstallOptInOptsInMultipleValueSetsWithSQLiteStore(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dir := t.TempDir()
	db, err := sqlite.OpenAndMigrate(ctx, filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()

	global := db.GlobalTerminologyStore()
	installs := db.TerminologyInstallStore("tenant-a")
	for _, rec := range []struct {
		url string
		raw []byte
	}{
		{"urn:vs1", []byte(`{"resourceType":"ValueSet","url":"urn:vs1","version":"1","status":"active"}`)},
		{"urn:vs2", []byte(`{"resourceType":"ValueSet","url":"urn:vs2","version":"1","status":"active"}`)},
	} {
		if err := global.PutResource(ctx, store.TerminologyResourceRecord{
			ScopeID: terminology.GlobalScopeID, ResourceType: "ValueSet",
			CanonicalURL: rec.url, Version: "1", ResourceJSON: rec.raw,
		}); err != nil {
			t.Fatal(err)
		}
		if err := terminology.EnsureInstallOptIn(ctx, installs, store.TerminologyInstallRecord{
			PackName: "pack-a", PackVersion: "1.0",
			ResourceType: "ValueSet", CanonicalURL: rec.url, Version: "1", Enabled: true,
		}); err != nil {
			t.Fatal(err)
		}
	}

	enabled, err := installs.ListEnabled(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(enabled) != 2 {
		t.Fatalf("enabled rows=%d want 2", len(enabled))
	}
}
