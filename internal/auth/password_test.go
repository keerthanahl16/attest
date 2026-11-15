package auth

import "testing"

func TestHashAndCheckPassword(t *testing.T) {
	hash, err := HashPassword("AttestDemo!123")
	if err != nil {
		t.Fatal(err)
	}
	if !CheckPassword(hash, "AttestDemo!123") {
		t.Fatal("expected password to match")
	}
	if CheckPassword(hash, "wrong") {
		t.Fatal("expected mismatch")
	}
}

func TestHashTokenStable(t *testing.T) {
	a := HashToken("atk_demo")
	b := HashToken("atk_demo")
	if a != b || a == "" {
		t.Fatalf("hash not stable: %q %q", a, b)
	}
}
