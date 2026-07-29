package postgres

import "testing"

func TestNormalizeSchema(t *testing.T) {
	t.Parallel()

	tests := []struct {
		in      string
		want    string
		wantErr bool
	}{
		{"", "public", false},
		{"public", "public", false},
		{"haistack", "haistack", false},
		{"fhir", "fhir", false},
		{"my_schema_1", "my_schema_1", false},
		{"1bad", "", true},
		{"bad-name", "", true},
	}
	for _, tc := range tests {
		got, err := normalizeSchema(tc.in)
		if tc.wantErr {
			if err == nil {
				t.Fatalf("normalizeSchema(%q) expected error", tc.in)
			}
			continue
		}
		if err != nil {
			t.Fatalf("normalizeSchema(%q): %v", tc.in, err)
		}
		if got != tc.want {
			t.Fatalf("normalizeSchema(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestDBSchemaHelpers(t *testing.T) {
	t.Parallel()

	public := dbSchema{name: "public"}
	if public.migrationsTable() != "hai_schema_migrations" {
		t.Fatalf("public migrations table: %q", public.migrationsTable())
	}
	if public.searchPath() != "public" {
		t.Fatalf("public search_path: %q", public.searchPath())
	}

	custom := dbSchema{name: "haistack"}
	if custom.migrationsTable() != "haistack.hai_schema_migrations" {
		t.Fatalf("custom migrations table: %q", custom.migrationsTable())
	}
	if custom.searchPath() != "haistack, public" {
		t.Fatalf("custom search_path: %q", custom.searchPath())
	}
}
