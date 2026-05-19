package auth

import (
	"strings"
	"testing"
	"time"

	"github.com/pquerna/otp/totp"
)

func TestVerifyTOTP(t *testing.T) {
	key, err := totp.Generate(totp.GenerateOpts{Issuer: "knowLib", AccountName: "me"})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	code, err := totp.GenerateCode(key.Secret(), now)
	if err != nil {
		t.Fatal(err)
	}
	if !VerifyTOTP(key.Secret(), code, now) {
		t.Fatal("expected valid TOTP to verify")
	}
	if VerifyTOTP(key.Secret(), "000000", now) {
		t.Fatal("expected obviously-wrong TOTP to fail")
	}
}

func TestSessionRoundTrip(t *testing.T) {
	tok, _, err := IssueSession("supersecret-1234567890abcdef", "me")
	if err != nil {
		t.Fatal(err)
	}
	c, err := parseSession("supersecret-1234567890abcdef", tok)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if c.Subject != "me" {
		t.Fatalf("subject=%q want me", c.Subject)
	}

	if _, err := parseSession("different-secret", tok); err == nil {
		t.Fatal("expected verify to fail with wrong secret")
	}
}

func TestAPITokenShape(t *testing.T) {
	raw, sha, err := MintAPIToken()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(raw, "ktok_") {
		t.Fatalf("expected ktok_ prefix, got %q", raw)
	}
	if !IsAPITokenShape(raw) {
		t.Fatal("IsAPITokenShape said no")
	}
	if HashAPIToken(raw) != sha {
		t.Fatal("hash mismatch")
	}
}
