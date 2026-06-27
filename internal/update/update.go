// Package update provides lightweight update-notification and
// self-update for ttl.
//
// Two surfaces:
//
//  1. Check() — asks GitHub's public API for the latest release tag.
//  2. MaybeNotice() — checks once per day, prints a one-line banner
//     to stderr if a newer release is available. Cached so it doesn't
//     hit the network on every command.
//
//  3. Apply() — downloads the right release binary for the running
//     OS/arch, verifies its SHA256 against the published SHA256SUMS,
//     and atomically replaces the currently-running executable.
//
// Privacy: the only network calls are unauthenticated GETs to
// api.github.com and objects.githubusercontent.com. No user data is
// sent. Disable with TTL_NO_UPDATE_CHECK=1.
package update

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// DefaultRepo is the canonical ttl repository. Overridable via the
// TTL_REPO env var so forks / mirrors can repoint.
const DefaultRepo = "anirudh-777/ttl"

// DefaultCheckInterval is how often MaybeNotice re-checks for a new
// release. Cached locally between runs.
const DefaultCheckInterval = 24 * time.Hour

// MinCheckInterval is the lower bound on the cooldown so a tight loop
// in CI doesn't hammer GitHub.
const MinCheckInterval = time.Hour

// httpClient is reused so a tight loop doesn't exhaust file
// descriptors. Always has a sane timeout.
var httpClient = &http.Client{Timeout: 5 * time.Second}

// GitHubLatest is the trimmed shape of api.github.com/repos/.../releases/latest.
type GitHubLatest struct {
	TagName string `json:"tag_name"`
}

// CheckResult is what Check returns. HasUpdate is true when remote
// > current, using simple semver-ish numeric compare on dotted
// segments (so "0.4.1" < "0.10.0" and "v0.4.1" == "0.4.1").
type CheckResult struct {
	Current   string
	Latest    string
	HasUpdate bool
}

// Check asks GitHub for the latest release tag. current is the
// running binary's version (with or without leading "v").
func Check(ctx context.Context, repo, current string) (CheckResult, error) {
	if repo == "" {
		repo = DefaultRepo
	}
	url := "https://api.github.com/repos/" + repo + "/releases/latest"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return CheckResult{}, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "ttl/"+current)
	resp, err := httpClient.Do(req)
	if err != nil {
		return CheckResult{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 256))
		return CheckResult{}, fmt.Errorf("github: %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}
	var lr GitHubLatest
	if err := json.NewDecoder(resp.Body).Decode(&lr); err != nil {
		return CheckResult{}, err
	}
	latest := strings.TrimPrefix(lr.TagName, "v")
	cur := strings.TrimPrefix(current, "v")
	return CheckResult{
		Current:   cur,
		Latest:    latest,
		HasUpdate: semverLess(cur, latest),
	}, nil
}

// cacheEntry is what we persist to ~/.config/ttl/.update-check
// (or platform equivalent) so we don't query GitHub on every run.
type cacheEntry struct {
	LastCheck   time.Time `json:"last_check"`
	LatestKnown string    `json:"latest_known"`
}

// cachePath returns the per-user file we use to remember the last
// check. Honour $XDG_CONFIG_HOME first, fall back to ~/.config.
func cachePath() (string, error) {
	dir := os.Getenv("XDG_CONFIG_HOME")
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		dir = filepath.Join(home, ".config")
	}
	full := filepath.Join(dir, "ttl", ".update-check")
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		return "", err
	}
	return full, nil
}

// readCache returns (entry, ok). ok=false if file missing or unreadable.
func readCache() (cacheEntry, bool) {
	path, err := cachePath()
	if err != nil {
		return cacheEntry{}, false
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return cacheEntry{}, false
	}
	var e cacheEntry
	if err := json.Unmarshal(b, &e); err != nil {
		return cacheEntry{}, false
	}
	return e, true
}

// writeCache persists entry; errors are intentionally swallowed —
// a broken cache just means we re-check on next run.
func writeCache(e cacheEntry) {
	path, err := cachePath()
	if err != nil {
		return
	}
	b, _ := json.Marshal(e)
	_ = os.WriteFile(path, b, 0o644)
}

// MaybeNotice prints a single line to stderr if a newer release is
// known and the cache is stale enough to re-check. Silent otherwise
// (no network, no version drift, or user opted out via
// TTL_NO_UPDATE_CHECK=1).
//
// interval defaults to DefaultCheckInterval; pass 0 to use the
// default. The function never panics and never returns an error —
// it's best-effort.
func MaybeNotice(current string, interval time.Duration) {
	if os.Getenv("TTL_NO_UPDATE_CHECK") != "" {
		return
	}
	if interval <= 0 {
		interval = DefaultCheckInterval
	}
	cache, _ := readCache()
	now := time.Now()
	// Use cached "latest" if it's still recent enough.
	if cache.LastCheck.Add(interval).After(now) && cache.LatestKnown != "" {
		if semverLess(current, cache.LatestKnown) {
			printNotice(current, cache.LatestKnown)
		}
		return
	}
	// Cache stale; hit the network. We don't gate this behind a mutex
	// because the result is process-local.
	res, err := Check(context.Background(), os.Getenv("TTL_REPO"), current)
	if err != nil {
		// Don't pollute the cache on failure — try again next run.
		return
	}
	writeCache(cacheEntry{LastCheck: now, LatestKnown: res.Latest})
	if res.HasUpdate {
		printNotice(current, res.Latest)
	}
}

// printNotice formats the one-line banner. Kept terse so it doesn't
// drown a CLI invocation in noise.
func printNotice(current, latest string) {
	exe, _ := os.Executable()
	installHint := "run `ttl update`"
	if exe != "" && !strings.Contains(exe, "go-build") {
		// On a real install, the binary knows where it lives.
		installHint = "run `" + filepath.Base(exe) + " update`"
	}
	fmt.Fprintf(os.Stderr,
		"\nttl: %s installed, %s available — %s\n\n",
		current, latest, installHint,
	)
}

// Platform returns "<os>-<arch>" matching the naming scheme used by
// scripts/build.sh and the published artefacts.
func Platform() string {
	os := runtime.GOOS
	arch := runtime.GOARCH
	return os + "-" + arch
}

// Apply downloads the release binary for the running platform,
// verifies its SHA256 against the published SHA256SUMS, and atomically
// replaces the running executable. repo defaults to DefaultRepo;
// tag may be "latest" or "vX.Y.Z".
func Apply(ctx context.Context, repo, tag, destPath string) error {
	if repo == "" {
		repo = DefaultRepo
	}
	if tag == "" || tag == "latest" {
		// We already have a TagName from Check, but re-fetch here so
		// Apply() is self-contained.
		res, err := Check(ctx, repo, "0.0.0")
		if err != nil {
			return fmt.Errorf("resolve latest: %w", err)
		}
		tag = "v" + res.Latest
	}
	if destPath == "" {
		var err error
		destPath, err = os.Executable()
		if err != nil {
			return err
		}
	}
	plat := Platform()
	ext := ""
	if runtime.GOOS == "windows" {
		ext = ".exe"
	}
	binURL := fmt.Sprintf("https://github.com/%s/releases/download/%s/ttl-%s%s",
		repo, tag, plat, ext)
	sumsURL := fmt.Sprintf("https://github.com/%s/releases/download/%s/SHA256SUMS",
		repo, tag)
	want := "ttl-" + plat + ext

	// Fetch SHA256SUMS first — fail fast if release is malformed.
	expected, err := lookupChecksum(ctx, sumsURL, want)
	if err != nil {
		return err
	}
	// Download to temp file in the same directory so the rename is
	// atomic (cross-filesystem rename is not).
	tmp, err := os.CreateTemp(filepath.Dir(destPath), "ttl-*.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath) // no-op if rename succeeded

	if err := downloadTo(ctx, binURL, tmp); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	// Verify before swapping.
	got, err := sha256File(tmpPath)
	if err != nil {
		return err
	}
	if got != expected {
		return fmt.Errorf("checksum mismatch: expected %s, got %s", expected, got)
	}
	if err := os.Chmod(tmpPath, 0o755); err != nil {
		return err
	}
	// Atomic on POSIX, best-effort on Windows (where open files can't
	// be replaced — Windows users should run ttl update when the
	// server is stopped).
	if err := os.Rename(tmpPath, destPath); err != nil {
		return err
	}
	return nil
}

// downloadTo streams resp.Body into w. Used for both the binary and
// SHA256SUMS files.
func downloadTo(ctx context.Context, url string, w io.Writer) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "ttl-update")
	resp, err := httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 256))
		return fmt.Errorf("download %s: %s: %s", url, resp.Status, strings.TrimSpace(string(body)))
	}
	_, err = io.Copy(w, resp.Body)
	return err
}

// lookupChecksum fetches SHA256SUMS and returns the line for `want`.
// Format is "<sha>  <filename>" per `shasum -a 256`.
func lookupChecksum(ctx context.Context, sumsURL, want string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, sumsURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "ttl-update")
	resp, err := httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("fetch SHA256SUMS: %s", resp.Status)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	for _, line := range strings.Split(string(body), "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 2 && fields[1] == want {
			return fields[0], nil
		}
	}
	return "", fmt.Errorf("no entry for %q in SHA256SUMS", want)
}

// sha256File computes the SHA-256 of a file.
func sha256File(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// semverLess reports whether a < b, comparing dotted numeric segments
// as integers. Pre-release tags ("0.4.1-rc1") sort before the
// release ("0.4.1") per semver §11.
func semverLess(a, b string) bool {
	if a == b {
		return false
	}
	if a == "" {
		return b != ""
	}
	if b == "" {
		return false
	}
	aNums, aPre, aTag := parseVersion(strings.TrimPrefix(a, "v"))
	bNums, bPre, bTag := parseVersion(strings.TrimPrefix(b, "v"))
	// Compare numeric segments one by one.
	n := len(aNums)
	if len(bNums) < n {
		n = len(bNums)
	}
	for i := 0; i < n; i++ {
		if aNums[i] != bNums[i] {
			return aNums[i] < bNums[i]
		}
	}
	if len(aNums) != len(bNums) {
		return len(aNums) < len(bNums)
	}
	// Numeric parts equal. Pre-release sorts before release.
	if aPre != bPre {
		return aPre
	}
	// Both same shape; compare pre-release tags.
	if aPre {
		return aTag < bTag
	}
	return false
}

// parseVersion splits a stripped ("v"-removed) version into
// (numeric segments, isPreRelease, preReleaseTag).
// "0.4.1"     -> ([0,4,1], false, "")
// "0.4.1-rc1" -> ([0,4,1], true,  "rc1")
// "0.4.1-rc.2"-> ([0,4,1], true,  "rc.2")
func parseVersion(s string) ([]int, bool, string) {
	// Split on '-' first to separate pre-release.
	main, pre, hasPre := strings.Cut(s, "-")
	nums := []int{}
	for _, p := range strings.Split(main, ".") {
		n, _ := strconv.Atoi(p)
		nums = append(nums, n)
	}
	return nums, hasPre, pre
}

// --- hash helpers removed; we use crypto/sha256 directly above.
