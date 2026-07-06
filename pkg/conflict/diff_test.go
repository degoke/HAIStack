package conflict_test

import (
	"testing"

	"github.com/degoke/health-ai-stack/pkg/conflict"
)

func TestDiffScalarReplace(t *testing.T) {
	base := resourceEnvelope(t, "Patient", "p1", "v1", map[string]any{
		"birthDate": "2000-01-01",
	})
	local := resourceEnvelope(t, "Patient", "p1", "v2", map[string]any{
		"birthDate": "2000-02-02",
	})

	eng := conflict.NewDefaultEngine()
	res := eng.Detect(localUpdate("Patient", "p1", "v1", "v2", local), base, base)

	if len(res.LocalChanges) != 1 {
		t.Fatalf("local changes = %d, want 1", len(res.LocalChanges))
	}
	if res.LocalChanges[0].Dotted("Patient") != "Patient.birthDate" {
		t.Fatalf("path = %q", res.LocalChanges[0].Dotted("Patient"))
	}
	if res.LocalChanges[0].Kind != conflict.ChangeKindScalarReplace {
		t.Fatalf("kind = %q", res.LocalChanges[0].Kind)
	}
}

func TestDiffArrayAppend(t *testing.T) {
	base := resourceEnvelope(t, "Patient", "p1", "v1", map[string]any{
		"telecom": []any{map[string]any{"system": "phone", "value": "111"}},
	})
	local := resourceEnvelope(t, "Patient", "p1", "v2", map[string]any{
		"telecom": []any{
			map[string]any{"system": "phone", "value": "111"},
			map[string]any{"system": "email", "value": "a@b"},
		},
	})

	eng := conflict.NewDefaultEngine()
	res := eng.Detect(localUpdate("Patient", "p1", "v1", "v2", local), base, base)

	if len(res.LocalChanges) != 1 {
		t.Fatalf("local changes = %d, want 1", len(res.LocalChanges))
	}
	if res.LocalChanges[0].Kind != conflict.ChangeKindArrayAppend {
		t.Fatalf("kind = %q", res.LocalChanges[0].Kind)
	}
	if len(res.LocalChanges[0].Appended) != 1 {
		t.Fatalf("appended = %d, want 1", len(res.LocalChanges[0].Appended))
	}
}

func TestDiffOverlapDetection(t *testing.T) {
	base := resourceEnvelope(t, "Patient", "p1", "v1", map[string]any{
		"telecom": []any{map[string]any{"system": "phone", "value": "111"}},
	})
	local := resourceEnvelope(t, "Patient", "p1", "v2", map[string]any{
		"telecom": []any{map[string]any{"system": "phone", "value": "222"}},
	})
	current := resourceEnvelope(t, "Patient", "p1", "v3", map[string]any{
		"telecom": []any{map[string]any{"system": "phone", "value": "333"}},
	})

	eng := conflict.NewDefaultEngine()
	res := eng.Detect(localUpdate("Patient", "p1", "v1", "v2", local), base, current)

	if len(res.OverlappingPaths) != 1 {
		t.Fatalf("overlaps = %v, want 1", res.OverlappingPaths)
	}
	if res.OverlappingPaths[0] != "Patient.telecom" {
		t.Fatalf("overlap = %q", res.OverlappingPaths[0])
	}
}

func TestDiffArrayStructural(t *testing.T) {
	base := resourceEnvelope(t, "Patient", "p1", "v1", map[string]any{
		"telecom": []any{map[string]any{"system": "phone", "value": "111"}},
	})
	local := resourceEnvelope(t, "Patient", "p1", "v2", map[string]any{
		"telecom": []any{map[string]any{"system": "email", "value": "a@b"}},
	})

	eng := conflict.NewDefaultEngine()
	res := eng.Detect(localUpdate("Patient", "p1", "v1", "v2", local), base, base)

	if len(res.LocalChanges) != 1 {
		t.Fatalf("local changes = %d, want 1", len(res.LocalChanges))
	}
	if res.LocalChanges[0].Kind != conflict.ChangeKindStructural {
		t.Fatalf("kind = %q, want structural", res.LocalChanges[0].Kind)
	}
}
