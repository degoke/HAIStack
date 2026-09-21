package researchutil

import "testing"

func TestHashBytesStable(t *testing.T) {
	a := HashBytes([]byte("haistack"))
	b := HashBytes([]byte("haistack"))
	if a == "" || a != b {
		t.Fatalf("hash drifted: %s vs %s", a, b)
	}
}

func TestParseResource(t *testing.T) {
	env, err := ParseResource("Patient", []byte(`{"resourceType":"Patient","id":"p1"}`))
	if err != nil {
		t.Fatal(err)
	}
	if env.ID != "p1" || env.Hash == "" {
		t.Fatalf("%+v", env)
	}
}
