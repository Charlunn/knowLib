// Package auth wires up TOTP login + JWT sessions + long-lived API tokens.
//
// Two credential kinds end up at the same middleware:
//   - Session JWT  (issued after a TOTP code) — short-lived, in HttpOnly cookie or Bearer
//   - API token    (long-lived, prefix "ktok_")  — issued via /api/tokens, used by MCP/skill
//
// The middleware accepts either; handlers can call CredKind(ctx) to distinguish.
package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/pquerna/otp/totp"
)

// === TOTP =====================================================================

// VerifyTOTP returns true if `code` (6 digits) matches the secret at `at`.
// Uses the default totp window (±1 step) so device clock drift up to 30s
// doesn't lock the user out.
func VerifyTOTP(secret, code string, at time.Time) bool {
	code = strings.TrimSpace(code)
	if len(code) != 6 {
		return false
	}
	ok, _ := totp.ValidateCustom(code, secret, at, totp.ValidateOpts{
		Period:    30,
		Skew:      1,
		Digits:    6,
		Algorithm: 0, // SHA1, the standard
	})
	return ok
}

// === JWT ======================================================================

const (
	sessionTTL  = 24 * time.Hour
	sessionKind = "session"
)

type SessionClaims struct {
	Kind string `json:"kind"`
	jwt.RegisteredClaims
}

func IssueSession(secret string, sub string) (string, time.Time, error) {
	now := time.Now()
	exp := now.Add(sessionTTL)
	claims := SessionClaims{
		Kind: sessionKind,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   sub,
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(exp),
		},
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	s, err := tok.SignedString([]byte(secret))
	return s, exp, err
}

func parseSession(secret, raw string) (*SessionClaims, error) {
	tok, err := jwt.ParseWithClaims(raw, &SessionClaims{}, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, errors.New("unexpected signing method")
		}
		return []byte(secret), nil
	})
	if err != nil {
		return nil, err
	}
	c, ok := tok.Claims.(*SessionClaims)
	if !ok || !tok.Valid || c.Kind != sessionKind {
		return nil, errors.New("invalid session token")
	}
	return c, nil
}

// === API tokens ===============================================================

const apiTokenPrefix = "ktok_"

// MintAPIToken returns (raw, sha256). Persist the sha256; show raw to the user
// exactly once. Length: 32 random bytes -> 43 base64url chars.
func MintAPIToken() (raw string, sha string, err error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", "", err
	}
	raw = apiTokenPrefix + base64.RawURLEncoding.EncodeToString(buf)
	sum := sha256.Sum256([]byte(raw))
	sha = hex.EncodeToString(sum[:])
	return raw, sha, nil
}

func HashAPIToken(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

func IsAPITokenShape(raw string) bool {
	return strings.HasPrefix(raw, apiTokenPrefix)
}

// === middleware ===============================================================

type credKey struct{}

type Cred struct {
	Kind    string // "session" | "api"
	Subject string
	TokenID string // for api tokens
}

func CredFrom(ctx context.Context) (Cred, bool) {
	c, ok := ctx.Value(credKey{}).(Cred)
	return c, ok
}

// TokenLookup resolves an API token sha256 to a (token_id, revoked?) pair.
// Implementations live in store.Tokens. We use a small interface here so
// the auth package doesn't depend on bbolt.
type TokenLookup interface {
	FindBySHA(sha string) (id string, revoked bool, ok bool)
}

func extractBearer(r *http.Request) string {
	if h := r.Header.Get("Authorization"); strings.HasPrefix(h, "Bearer ") {
		return strings.TrimSpace(strings.TrimPrefix(h, "Bearer "))
	}
	if c, err := r.Cookie("klib_session"); err == nil {
		return c.Value
	}
	return ""
}

// RequireAuth accepts either a session JWT or a non-revoked API token.
func RequireAuth(jwtSecret string, tokens TokenLookup) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			raw := extractBearer(r)
			if raw == "" {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			var cred Cred
			if IsAPITokenShape(raw) {
				id, revoked, ok := tokens.FindBySHA(HashAPIToken(raw))
				if !ok || revoked {
					http.Error(w, "unauthorized", http.StatusUnauthorized)
					return
				}
				cred = Cred{Kind: "api", TokenID: id, Subject: "api:" + id}
			} else {
				c, err := parseSession(jwtSecret, raw)
				if err != nil {
					http.Error(w, "unauthorized", http.StatusUnauthorized)
					return
				}
				cred = Cred{Kind: "session", Subject: c.Subject}
			}
			ctx := context.WithValue(r.Context(), credKey{}, cred)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}
