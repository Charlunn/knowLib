package handlers

import (
	"archive/zip"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/charlunn/knowlib/api/internal/auth"
)

// SkillBundle zips packages/skill-bundle/, substituting {{SERVER_URL}} and
// {{API_TOKEN}} so the user gets a personalised drop-in.
//
// The token minted here is fresh per download and saved to bbolt with name
// "skill-bundle-<timestamp>" so the user can revoke individual downloads.
func SkillBundle(d *Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cred, _ := auth.CredFrom(r.Context())
		if cred.Kind != "session" {
			writeError(w, http.StatusForbidden, "skill bundle download requires session login")
			return
		}
		raw, sha, err := auth.MintAPIToken()
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		name := fmt.Sprintf("skill-bundle-%s", time.Now().UTC().Format("20060102T150405Z"))
		if _, err := d.Store.CreateToken(name, sha); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}

		serverURL := "https://" + d.Cfg.Domain
		root := d.Cfg.SkillBundleDir
		if _, err := os.Stat(root); err != nil {
			writeError(w, http.StatusInternalServerError, "skill bundle template not mounted: "+err.Error())
			return
		}

		w.Header().Set("Content-Type", "application/zip")
		w.Header().Set("Content-Disposition", `attachment; filename="knowlib-skill-bundle.zip"`)
		zw := zip.NewWriter(w)
		defer zw.Close()

		err = filepath.WalkDir(root, func(p string, de fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			rel, err := filepath.Rel(root, p)
			if err != nil {
				return err
			}
			if rel == "." {
				return nil
			}
			rel = filepath.ToSlash(rel)
			if de.IsDir() {
				_, err := zw.Create(rel + "/")
				return err
			}
			fh, err := os.Open(p)
			if err != nil {
				return err
			}
			defer fh.Close()
			zfh, err := zw.Create(rel)
			if err != nil {
				return err
			}
			// Substitute placeholders in text-ish files (.md, .json, .js, .yml, .yaml).
			if isTextish(rel) {
				body, err := io.ReadAll(fh)
				if err != nil {
					return err
				}
				body = []byte(strings.ReplaceAll(string(body), "{{SERVER_URL}}", serverURL))
				body = []byte(strings.ReplaceAll(string(body), "{{API_TOKEN}}", raw))
				_, err = zfh.Write(body)
				return err
			}
			_, err = io.Copy(zfh, fh)
			return err
		})
		if err != nil {
			// We've already started writing zip headers; can't switch to JSON 500.
			// Best we can do is close the writer; the client will see a corrupt zip.
			return
		}
	}
}

func isTextish(name string) bool {
	for _, ext := range []string{".md", ".json", ".js", ".yml", ".yaml", ".txt", ".sh"} {
		if strings.HasSuffix(name, ext) {
			return true
		}
	}
	return false
}
