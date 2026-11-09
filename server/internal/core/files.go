package core

import (
	"context"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/narendersinghh/setu/server/internal/app"
	"github.com/narendersinghh/setu/server/internal/secure"
	"github.com/narendersinghh/setu/server/internal/settings"
)

// Settings that are managed by the platform, never through the generic settings API.
var internalKey = regexp.MustCompile(`^(mod_|cron_|db_version|installed_|update_|scheduler|queue_|workers$)`)

func registerSettings(a *app.App) {
	a.Get("/settings", settingsGet, app.Perm("settings"))
	a.Put("/settings", settingsPut, app.Perm("settings"))
	a.Post("/files", upload, app.Perm("settings"))
	a.Public(http.MethodGet, "/files/{org}/{name}", serveFile)
}

func settingsGet(c *app.Ctx) (any, error) { return c.S().Public(), nil }

// settingsPut saves any number of keys. A masked secret ("••••••••") means "unchanged".
func settingsPut(c *app.Ctx) (any, error) {
	var in map[string]any
	if err := c.Bind(&in); err != nil {
		return nil, err
	}
	for k, v := range in {
		if internalKey.MatchString(k) || len(k) > 80 || k == "" {
			continue
		}
		var s string
		switch x := v.(type) {
		case string:
			s = x
		case bool:
			s = map[bool]string{true: "1", false: "0"}[x]
		case float64:
			s = strconv.FormatFloat(x, 'f', -1, 64)
		case nil:
			s = ""
		default:
			if err := c.SetS(k, x); err != nil {
				return nil, err
			}
			continue
		}
		if settings.IsSecret(k) && s == settings.Masked {
			continue
		}
		if err := c.SetS(k, s); err != nil {
			return nil, err
		}
	}
	return c.S().Public(), nil
}

var okExt = map[string]bool{".png": true, ".jpg": true, ".jpeg": true, ".webp": true, ".gif": true, ".svg": true, ".pdf": true, ".csv": true}

// SaveFile stores an upload under storage/files/{org}/ with a random name; returns its public path.
func SaveFile(c context.Context, a *app.App, orgID int64, name string, r io.Reader, maxBytes int64) (string, error) {
	ext := strings.ToLower(filepath.Ext(name))
	if !okExt[ext] {
		return "", app.Bad("File type not allowed.")
	}
	dir := filepath.Join(a.Cfg.StorageDir, "files", strconv.FormatInt(orgID, 10))
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return "", err
	}
	fn := secure.Token(16) + ext
	f, err := os.OpenFile(filepath.Join(dir, fn), os.O_CREATE|os.O_WRONLY|os.O_EXCL, 0o640)
	if err != nil {
		return "", err
	}
	defer f.Close()
	n, err := io.Copy(f, io.LimitReader(r, maxBytes+1))
	if err != nil {
		return "", err
	}
	if n > maxBytes {
		os.Remove(f.Name())
		return "", app.Bad("File is too large.")
	}
	return "/files/" + strconv.FormatInt(orgID, 10) + "/" + fn, nil
}

func upload(c *app.Ctx) (any, error) {
	if err := c.R.ParseMultipartForm(12 << 20); err != nil {
		return nil, app.Bad("Upload failed.")
	}
	f, h, err := c.R.FormFile("file")
	if err != nil {
		return nil, app.Bad("Choose a file.")
	}
	defer f.Close()
	p, err := SaveFile(c, c.A, c.Org.ID, h.Filename, f, 10<<20)
	if err != nil {
		return nil, err
	}
	if k := c.R.FormValue("setting"); k != "" && !internalKey.MatchString(k) && !settings.IsSecret(k) {
		if err := c.SetS(k, p); err != nil {
			return nil, err
		}
	}
	return map[string]any{"ok": true, "url": p}, nil
}

var fileName = regexp.MustCompile(`^[a-f0-9]{32}\.[a-z]{3,4}$`)

// serveFile serves uploads; names are 128-bit random so links cannot be guessed.
func serveFile(c *app.Ctx) (any, error) {
	org, name := c.Param("org"), c.Param("name")
	if _, err := strconv.ParseInt(org, 10, 64); err != nil || !fileName.MatchString(name) {
		return nil, app.NotFound()
	}
	c.Raw()
	c.W.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	if strings.HasSuffix(name, ".svg") {
		c.W.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'")
	}
	http.ServeFile(c.W, c.R, filepath.Join(c.A.Cfg.StorageDir, "files", org, name))
	return nil, nil
}
