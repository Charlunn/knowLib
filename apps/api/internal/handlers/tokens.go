package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/charlunn/knowlib/api/internal/auth"
	"github.com/charlunn/knowlib/api/internal/store"
	"github.com/go-chi/chi/v5"
)

// Tokens — list / mint / revoke long-lived API tokens used by MCP / skill bundle.
// Only sessions (interactive) can manage tokens; tokens cannot manage tokens.

func ListTokens(d *Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		recs, err := d.Store.ListTokens()
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		// Hide sha — useless to clients, opens phishing surface.
		out := make([]map[string]any, 0, len(recs))
		for _, rec := range recs {
			out = append(out, sanitizeToken(rec))
		}
		writeJSON(w, http.StatusOK, out)
	}
}

func CreateToken(d *Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cred, _ := auth.CredFrom(r.Context())
		if cred.Kind != "session" {
			writeError(w, http.StatusForbidden, "token management requires session login")
			return
		}
		var in struct {
			Name string `json:"name"`
		}
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			writeError(w, http.StatusBadRequest, "bad json")
			return
		}
		if in.Name == "" {
			in.Name = "unnamed"
		}
		raw, sha, err := auth.MintAPIToken()
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		rec, err := d.Store.CreateToken(in.Name, sha)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		// Show the raw token ONCE.
		writeJSON(w, http.StatusCreated, map[string]any{
			"id":         rec.ID,
			"name":       rec.Name,
			"created_at": rec.CreatedAt,
			"token":      raw,
			"warning":    "save this token now — it won't be shown again",
		})
	}
}

func RevokeToken(d *Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cred, _ := auth.CredFrom(r.Context())
		if cred.Kind != "session" {
			writeError(w, http.StatusForbidden, "token management requires session login")
			return
		}
		id := chi.URLParam(r, "id")
		if err := d.Store.RevokeToken(id); err != nil {
			// store.RevokeToken returns a sentinel-ish "token not found" error;
			// match by string since the package doesn't export it as a typed value.
			if err.Error() == "token not found" {
				writeError(w, http.StatusNotFound, "not found")
				return
			}
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

func sanitizeToken(rec store.TokenRecord) map[string]any {
	return map[string]any{
		"id":           rec.ID,
		"name":         rec.Name,
		"created_at":   rec.CreatedAt,
		"last_used_at": rec.LastUsedAt,
		"revoked_at":   rec.RevokedAt,
	}
}
