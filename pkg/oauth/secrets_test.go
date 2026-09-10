package oauth

import "testing"

func TestSecretEqual(t *testing.T) {
	if !secretEqual("abc", "abc") {
		t.Fatal("expected equal secrets to match")
	}
	if secretEqual("abc", "abd") {
		t.Fatal("expected different secrets to fail")
	}
	if secretEqual("abc", "abcd") {
		t.Fatal("expected length mismatch to fail")
	}
}

func TestTokenEqual(t *testing.T) {
	if !TokenEqual("csrf-token", "csrf-token") {
		t.Fatal("expected equal tokens to match")
	}
	if TokenEqual("csrf-token", "csrf-tokex") {
		t.Fatal("expected different tokens to fail")
	}
}
