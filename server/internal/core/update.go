package core

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/narendersinghh/setu/server/internal/app"
	"github.com/narendersinghh/setu/server/internal/buildinfo"
	"github.com/narendersinghh/setu/server/internal/netx"
)

var (
	hexColor = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)
	fontName = regexp.MustCompile(`^[A-Za-z0-9 ]{0,40}$`)
	repoRe   = regexp.MustCompile(`^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$`)
)

// In-app updates from GitHub Releases (built by .github/workflows/release.yml). The running binary
// downloads the asset for its OS/arch, verifies the published sha256, replaces itself and exits;
// systemd starts the new version, which runs the database migrations.

func registerUpdater(a *app.App) {
	p := app.PlatformOnly()
	a.Get("/platform/update", updateStatus, p)
	a.Put("/platform/update", updateSettings, p)
	a.Post("/platform/update/check", func(c *app.Ctx) (any, error) { return checkUpdate(c, c.A, true) }, p)
	a.Post("/platform/update/apply", applyUpdate, p)
}

func updRepo(ctx context.Context, a *app.App) string {
	return PGet(ctx, a, "update_repo", a.Cfg.UpdateRepo)
}
func updChannel(ctx context.Context, a *app.App) string {
	return PGet(ctx, a, "update_channel", a.Cfg.UpdateChan)
}
func updToken(ctx context.Context, a *app.App) string {
	return PGet(ctx, a, "update_token", a.Cfg.UpdateToken)
}

func assetName() string { return "setu-" + runtime.GOOS + "-" + runtime.GOARCH }

func ghHeaders(tok, accept string) map[string]string {
	h := map[string]string{"Accept": accept, "X-GitHub-Api-Version": "2022-11-28"}
	if tok != "" {
		h["Authorization"] = "Bearer " + tok
	}
	return h
}

type ghRelease struct {
	Tag       string `json:"tag_name"`
	Name      string `json:"name"`
	Body      string `json:"body"`
	Published string `json:"published_at"`
	HTMLURL   string `json:"html_url"`
	Assets    []struct {
		Name string `json:"name"`
		URL  string `json:"url"`
		Size int64  `json:"size"`
	} `json:"assets"`
}

var updMu sync.Mutex
var lastCheck map[string]any

// selfSHA is the checksum of the running binary (compared with the release asset's checksum).
func selfSHA() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	f, err := os.Open(exe)
	if err != nil {
		return ""
	}
	defer f.Close()
	h := sha256.New()
	io.Copy(h, f)
	return hex.EncodeToString(h.Sum(nil))
}

func updateStatus(c *app.Ctx) (any, error) {
	exe, _ := os.Executable()
	out := map[string]any{"version": buildinfo.Version, "commit": buildinfo.Commit, "repo": updRepo(c, c.A), "channel": updChannel(c, c.A),
		"token_set": updToken(c, c.A) != "", "asset": assetName(), "can_self_update": canSelfUpdate(exe) == nil, "binary": exe}
	if err := canSelfUpdate(exe); err != nil {
		out["self_update_note"] = err.Error()
	}
	updMu.Lock()
	out["last"] = lastCheck
	updMu.Unlock()
	return out, nil
}

func updateSettings(c *app.Ctx) (any, error) {
	var in struct{ Repo, Channel, Token string }
	if err := c.Bind(&in); err != nil {
		return nil, err
	}
	in.Repo = strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(strings.TrimSpace(in.Repo), "https://github.com/"), ".git"))
	if !repoRe.MatchString(in.Repo) {
		return nil, app.Bad("Repository must look like owner/name.")
	}
	if in.Channel != "stable" && in.Channel != "edge" {
		in.Channel = "stable"
	}
	kv := map[string]string{"update_repo": in.Repo, "update_channel": in.Channel}
	if in.Token != "" {
		kv["update_token"] = in.Token
	}
	return nil, PSet(c, c.A, kv)
}

func fetchRelease(ctx context.Context, a *app.App) (*ghRelease, error) {
	u := "https://api.github.com/repos/" + updRepo(ctx, a) + "/releases/latest"
	if updChannel(ctx, a) == "edge" {
		u = "https://api.github.com/repos/" + updRepo(ctx, a) + "/releases/tags/edge"
	}
	var r ghRelease
	res, err := netx.GetJSON(ctx, u, ghHeaders(updToken(ctx, a), "application/vnd.github+json"), &r)
	if err != nil {
		if res != nil && res.Code == 404 {
			return nil, fmt.Errorf("no release found on %s (private repository? add a GitHub token)", updRepo(ctx, a))
		}
		return nil, err
	}
	switch res.Code {
	case 200:
		return &r, nil
	case 404:
		return nil, fmt.Errorf("no %s release found on %s yet – publish one (git tag v2.0.0 && git push --tags), and add a GitHub token if the repository is private", updChannel(ctx, a), updRepo(ctx, a))
	case 401, 403:
		return nil, fmt.Errorf("GitHub refused access (HTTP %d) – check the GitHub token", res.Code)
	}
	return nil, fmt.Errorf("GitHub returned HTTP %d", res.Code)
}

func download(ctx context.Context, a *app.App, url string) ([]byte, error) {
	r, err := netx.Do(ctx, netx.Req{URL: url, Headers: ghHeaders(updToken(ctx, a), "application/octet-stream"), Timeout: 5 * time.Minute, Max: 300 << 20})
	if err != nil {
		return nil, err
	}
	if r.Code != 200 {
		return nil, fmt.Errorf("download failed: HTTP %d", r.Code)
	}
	return r.Body, nil
}

// checkUpdate compares the running binary with the latest release asset (by sha256).
func checkUpdate(c context.Context, a *app.App, force bool) (map[string]any, error) {
	rel, err := fetchRelease(c, a)
	if err != nil {
		return nil, app.Bad("Update check failed: " + err.Error())
	}
	var sumURL string
	for _, as := range rel.Assets {
		if as.Name == assetName()+".sha256" {
			sumURL = as.URL
		}
	}
	if sumURL == "" {
		return nil, app.Bad("The release has no " + assetName() + " build yet.")
	}
	sum, err := download(c, a, sumURL)
	if err != nil {
		return nil, app.Bad(err.Error())
	}
	remote := strings.Fields(string(sum))
	if len(remote) == 0 {
		return nil, app.Bad("Invalid checksum file in the release.")
	}
	out := map[string]any{"tag": rel.Tag, "name": rel.Name, "notes": rel.Body, "published": rel.Published, "url": rel.HTMLURL,
		"available": remote[0] != selfSHA(), "checked_at": time.Now(), "sha256": remote[0]}
	updMu.Lock()
	lastCheck = out
	updMu.Unlock()
	return out, nil
}

func canSelfUpdate(exe string) error {
	if _, err := os.Stat("/.dockerenv"); err == nil {
		return fmt.Errorf("running in Docker – update by pulling the new image (docker compose up -d --build)")
	}
	dir := filepath.Dir(exe)
	f, err := os.CreateTemp(dir, ".setu-write-test-*")
	if err != nil {
		return fmt.Errorf("the program folder %s is not writable – run deploy/update.sh as root", dir)
	}
	f.Close()
	os.Remove(f.Name())
	return nil
}

func applyUpdate(c *app.Ctx) (any, error) {
	if !updMu.TryLock() {
		return nil, app.Bad("An update is already running.")
	}
	defer updMu.Unlock()
	exe, err := os.Executable()
	if err != nil {
		return nil, err
	}
	if exe, err = filepath.EvalSymlinks(exe); err != nil {
		return nil, err
	}
	if err := canSelfUpdate(exe); err != nil {
		return nil, app.Bad(err.Error())
	}
	rel, err := fetchRelease(c, c.A)
	if err != nil {
		return nil, app.Bad(err.Error())
	}
	var binURL, sumURL string
	for _, as := range rel.Assets {
		switch as.Name {
		case assetName():
			binURL = as.URL
		case assetName() + ".sha256":
			sumURL = as.URL
		}
	}
	if binURL == "" || sumURL == "" {
		return nil, app.Bad("The release has no " + assetName() + " build.")
	}
	sum, err := download(c, c.A, sumURL)
	if err != nil {
		return nil, app.Bad(err.Error())
	}
	bin, err := download(c, c.A, binURL)
	if err != nil {
		return nil, app.Bad(err.Error())
	}
	h := sha256.Sum256(bin)
	want := strings.Fields(string(sum))
	if len(want) == 0 || hex.EncodeToString(h[:]) != want[0] {
		return nil, app.Bad("Checksum mismatch – the download is incomplete or was changed. Nothing was installed.")
	}
	if !bytes.HasPrefix(bin, []byte("\x7fELF")) && runtime.GOOS == "linux" {
		return nil, app.Bad("The downloaded file is not a Linux program.")
	}
	tmp := exe + ".new"
	if err := os.WriteFile(tmp, bin, 0o755); err != nil {
		return nil, app.Bad("Could not write the new version: " + err.Error())
	}
	os.Rename(exe, exe+".old") // keep the previous version for a manual rollback
	if err := os.Rename(tmp, exe); err != nil {
		os.Rename(exe+".old", exe)
		return nil, app.Bad("Could not replace the program: " + err.Error())
	}
	slog.Info("update installed – restarting", "tag", rel.Tag, "by", c.User.Email)
	// Exit after the response is sent; systemd (Restart=always) starts the new version
	c.After(func() {
		go func() {
			time.Sleep(1500 * time.Millisecond)
			os.Exit(0)
		}()
	})
	return map[string]any{"ok": true, "message": "Updated to " + rel.Tag + ". " + c.A.BrandName(c) + " restarts now – reload the page in a few seconds."}, nil
}
