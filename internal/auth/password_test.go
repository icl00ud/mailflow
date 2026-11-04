package auth

import (
	"strings"
	"testing"
	"time"
)

func TestPasswordRoundTrip(t *testing.T) {
	hash, err := HashPassword("correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(hash, "$argon2id$v=19$m=65536,t=3,p=4$") {
		t.Fatalf("unexpected hash format: %s", hash)
	}
	if !VerifyPassword(hash, "correct horse battery staple") {
		t.Fatal("valid password rejected")
	}
	if VerifyPassword(hash, "correct horse battery staple ") {
		t.Fatal("wrong password accepted")
	}
	if VerifyPassword(hash, "") {
		t.Fatal("empty password accepted")
	}
}

func TestPasswordUniqueSalt(t *testing.T) {
	a, _ := HashPassword("same")
	b, _ := HashPassword("same")
	if a == b {
		t.Fatal("two hashes of the same password are identical: salt is not random")
	}
	if !VerifyPassword(a, "same") || !VerifyPassword(b, "same") {
		t.Fatal("one of two hashes failed to verify")
	}
}

func TestVerifyRejectsGarbage(t *testing.T) {
	for _, h := range []string{"", "plain", "$argon2i$v=19$m=1,t=1,p=1$aa$bb",
		"$argon2id$v=99$m=65536,t=3,p=4$YQ$YQ", "$argon2id$v=19$m=x,t=3,p=4$YQ$YQ",
		"$argon2id$v=19$m=65536,t=3,p=4$!!!$YQ"} {
		if VerifyPassword(h, "anything") {
			t.Fatalf("garbage hash accepted: %q", h)
		}
	}
	if VerifyPassword(DummyHash, "anything") {
		t.Fatal("dummy hash accepted a password")
	}
}

func TestVerifyRejectsHugeCost(t *testing.T) {
	// A row with an absurd memory setting must be refused, not allocate 4 GiB.
	if VerifyPassword("$argon2id$v=19$m=4194304,t=3,p=4$YQ$YQ", "x") {
		t.Fatal("hostile cost parameters accepted")
	}
}

func TestTokenHash(t *testing.T) {
	tok, err := NewToken()
	if err != nil {
		t.Fatal(err)
	}
	if len(tok) < 40 {
		t.Fatalf("token too short: %q", tok)
	}
	if tok == HashToken(tok) || HashToken(tok) == HashToken(tok+"x") {
		t.Fatal("token hash is not a hash")
	}
	other, _ := NewToken()
	if tok == other {
		t.Fatal("two tokens are identical")
	}
}

func TestLimiter(t *testing.T) {
	now := time.Date(2026, 11, 3, 12, 0, 0, 0, time.UTC)
	l := NewLimiter(3, 15*time.Minute)
	for i := 0; i < 3; i++ {
		if !l.Allow("ip|a@b.co", now) {
			t.Fatalf("attempt %d refused too early", i+1)
		}
		l.Fail("ip|a@b.co", now)
	}
	if l.Allow("ip|a@b.co", now) {
		t.Fatal("4th attempt allowed")
	}
	if !l.Allow("otherip|a@b.co", now) {
		t.Fatal("a different ip must have its own budget")
	}
	if !l.Allow("ip|a@b.co", now.Add(15*time.Minute+time.Second)) {
		t.Fatal("window did not expire")
	}
	l.Fail("ip|a@b.co", now.Add(16*time.Minute))
	l.Reset("ip|a@b.co")
	if !l.Allow("ip|a@b.co", now.Add(16*time.Minute)) {
		t.Fatal("reset did not clear history")
	}
}
