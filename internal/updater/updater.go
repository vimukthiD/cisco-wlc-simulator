// Package updater implements the OVA appliance's in-place system update: it
// checks GitHub for a newer stable release, downloads and verifies the new
// binaries, and swaps them in via a detached helper that health-checks the
// restart and automatically rolls back on failure. It is inert off-appliance.
package updater

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	"github.com/vimukthiD/cisco-wlc-simulator/internal/accesslog"
)

// Defaults for the appliance environment.
const (
	defaultRepo      = "vimukthiD/cisco-wlc-simulator"
	defaultBinDir    = "/usr/local/bin"
	defaultWorkDir   = "/var/lib/wlcsim/update"
	updateLogPath    = "/var/log/wlcsim-update.log"
	initScriptPath   = "/etc/init.d/wlcsim"
	serviceName      = "wlcsim"
	minFreeBytes     = 150 << 20 // ~2 binaries staged + margin
	healthTimeoutSec = 40
	checkDebounce    = 10 * time.Second
	resultTTL        = 15 * time.Minute
)

// Sentinel errors mapped to HTTP status codes by the dashboard.
var (
	ErrNotAppliance      = errors.New("system update is only available on the appliance")
	ErrAlreadyInProgress = errors.New("an update is already in progress")
	ErrNoUpdateAvailable = errors.New("already on the latest release")
	ErrUnsupportedArch   = errors.New("unsupported CPU architecture for update")
	ErrInsufficientSpace = errors.New("insufficient free disk space for update")
)

// Options configures an Updater. Zero values fall back to the appliance defaults.
type Options struct {
	Repo          string
	BinDir        string
	WorkDir       string
	DashboardPort int
	HTTPClient    *http.Client
}

// Status is the JSON payload returned by the dashboard's update endpoints.
type Status struct {
	CurrentVersion  string     `json:"current_version"`
	LatestVersion   string     `json:"latest_version,omitempty"`
	ReleaseURL      string     `json:"release_url,omitempty"`
	UpdateAvailable bool       `json:"update_available"`
	Appliance       bool       `json:"appliance"`
	InProgress      bool       `json:"in_progress"`
	LastChecked     *time.Time `json:"last_checked,omitempty"`
	LastError       string     `json:"last_error,omitempty"`
	LastResult      *Result    `json:"last_result,omitempty"`
}

// Updater owns the update lifecycle. Its methods are safe for concurrent use.
type Updater struct {
	version       string
	appliance     bool
	repo          string
	binDir        string
	workDir       string
	dashboardPort int
	hc            *http.Client
	logs          *accesslog.Store

	busy atomic.Bool

	mu          sync.Mutex
	latest      *Release
	lastChecked time.Time
	lastErr     string
}

// New builds an Updater. currentVersion is the running build's version string
// (main.version). logs receives coarse progress entries so the dashboard's live
// log panel doubles as the update progress view.
func New(currentVersion string, logs *accesslog.Store, opts Options) *Updater {
	if opts.Repo == "" {
		opts.Repo = defaultRepo
	}
	if opts.BinDir == "" {
		opts.BinDir = defaultBinDir
	}
	if opts.WorkDir == "" {
		opts.WorkDir = defaultWorkDir
	}
	if opts.DashboardPort == 0 {
		opts.DashboardPort = 8080
	}
	if opts.HTTPClient == nil {
		opts.HTTPClient = &http.Client{Timeout: 5 * time.Minute}
	}
	return &Updater{
		version:       currentVersion,
		appliance:     detectAppliance(),
		repo:          opts.Repo,
		binDir:        opts.BinDir,
		workDir:       opts.WorkDir,
		dashboardPort: opts.DashboardPort,
		hc:            opts.HTTPClient,
		logs:          logs,
	}
}

// detectAppliance reports whether we're running as the OVA appliance service.
// The init script is baked into the rootfs and untouched by updates, so this
// stays correct after a self-update. WLCSIM_APPLIANCE=1 forces it on for local
// testing.
func detectAppliance() bool {
	if os.Getenv("WLCSIM_APPLIANCE") == "1" {
		return true
	}
	info, err := os.Stat(initScriptPath)
	return err == nil && !info.IsDir()
}

// IsAppliance reports whether update actions are permitted in this environment.
func (u *Updater) IsAppliance() bool { return u.appliance }

// resultPath is where the helper records the outcome of the last update.
func (u *Updater) resultPath() string {
	return filepath.Join(u.workDir, "last-result.json")
}

// Status returns a snapshot of the current update state. It performs no
// network I/O; it does read the small on-disk result file so a post-restart
// page can surface the previous update's outcome.
func (u *Updater) Status() Status {
	u.mu.Lock()
	st := Status{
		CurrentVersion: u.version,
		Appliance:      u.appliance,
		InProgress:     u.busy.Load(),
		LastError:      u.lastErr,
	}
	if u.latest != nil {
		st.LatestVersion = u.latest.TagName
		st.ReleaseURL = u.latest.HTMLURL
		st.UpdateAvailable = u.appliance && isNewer(u.latest.TagName, u.version)
	}
	if !u.lastChecked.IsZero() {
		t := u.lastChecked
		st.LastChecked = &t
	}
	u.mu.Unlock()

	if res, ok := readResult(u.resultPath()); ok && time.Since(res.Timestamp) < resultTTL {
		st.LastResult = res
	}
	return st
}

// CheckLatest queries GitHub for the latest release and caches the result.
// Repeated calls within checkDebounce return the cached release.
func (u *Updater) CheckLatest(ctx context.Context) (*Release, error) {
	u.mu.Lock()
	if u.latest != nil && time.Since(u.lastChecked) < checkDebounce {
		rel := u.latest
		u.mu.Unlock()
		return rel, nil
	}
	u.mu.Unlock()

	rel, err := fetchLatestRelease(ctx, u.repo, u.hc)

	u.mu.Lock()
	u.lastChecked = time.Now()
	if err != nil {
		u.lastErr = err.Error()
		u.mu.Unlock()
		return nil, err
	}
	u.latest = rel
	u.lastErr = ""
	u.mu.Unlock()
	return rel, nil
}

// Apply validates preconditions synchronously and, if they pass, kicks off the
// download/verify/stage/restart flow in the background, returning immediately.
func (u *Updater) Apply(ctx context.Context) error {
	if !u.appliance {
		return ErrNotAppliance
	}
	if !u.busy.CompareAndSwap(false, true) {
		return ErrAlreadyInProgress
	}

	// From here, any early return must clear the busy flag.
	release, err := u.releaseForApply(ctx)
	if err != nil {
		u.busy.Store(false)
		return err
	}
	if _, ok := archAssetSuffix(); !ok {
		u.busy.Store(false)
		return ErrUnsupportedArch
	}
	if free, err := freeBytes(u.binDir); err == nil && free < minFreeBytes {
		u.busy.Store(false)
		return ErrInsufficientSpace
	}

	go u.run(release)
	return nil
}

// releaseForApply returns a newer release than the running build, refreshing
// the cache if needed.
func (u *Updater) releaseForApply(ctx context.Context) (*Release, error) {
	u.mu.Lock()
	rel := u.latest
	u.mu.Unlock()
	if rel == nil {
		var err error
		if rel, err = u.CheckLatest(ctx); err != nil {
			return nil, err
		}
	}
	if !isNewer(rel.TagName, u.version) {
		return nil, ErrNoUpdateAvailable
	}
	return rel, nil
}

// run downloads and verifies both binaries, then hands off to the detached
// helper. It runs in its own goroutine. Progress is streamed to the log store.
func (u *Updater) run(rel *Release) {
	// Keep the busy flag set (and the staged binaries in place) once the helper
	// is spawned: the process stays "in progress" until the helper restarts it,
	// at which point the fresh process starts with busy=false. Any failure
	// *before* handoff clears the flag and discards the staged files, so a
	// stalled/failed pre-restart attempt frees a retry and leaves nothing behind.
	spawned := false
	newWlcsim := filepath.Join(u.workDir, "wlcsim.new")
	newConsole := filepath.Join(u.workDir, "wlcsim-console.new")
	defer func() {
		if spawned {
			return
		}
		u.busy.Store(false)
		os.Remove(newWlcsim)
		os.Remove(newConsole)
	}()

	suffix, _ := archAssetSuffix()
	wlcsimAsset := "wlcsim-" + suffix
	consoleAsset := "wlcsim-console-" + suffix

	u.logf("Starting update to %s", rel.TagName)

	// Resolve required assets up front.
	wa, ok := rel.asset(wlcsimAsset)
	if !ok {
		u.fail("release %s is missing asset %s", rel.TagName, wlcsimAsset)
		return
	}
	ca, ok := rel.asset(consoleAsset)
	if !ok {
		u.fail("release %s is missing asset %s", rel.TagName, consoleAsset)
		return
	}
	cs, ok := rel.asset("checksums.txt")
	if !ok {
		u.fail("release %s is missing checksums.txt", rel.TagName)
		return
	}

	if err := os.MkdirAll(u.workDir, 0o755); err != nil {
		u.fail("cannot create work dir: %v", err)
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	u.logf("Fetching checksums")
	sums, err := fetchChecksums(ctx, cs.BrowserDownloadURL, u.hc)
	if err != nil {
		u.fail("checksums: %v", err)
		return
	}

	if err := u.fetchVerify(ctx, wa, newWlcsim, sums); err != nil {
		u.fail("%v", err)
		return
	}
	if err := u.fetchVerify(ctx, ca, newConsole, sums); err != nil {
		u.fail("%v", err)
		return
	}

	// Stage the helper config and hand off. Nothing under binDir has been
	// touched yet — the helper does the swap so a failed download is a no-op.
	cfg := helperConfig{
		ServiceName:     serviceName,
		HealthURL:       fmt.Sprintf("http://127.0.0.1:%d/api/system", u.dashboardPort),
		HealthTimeout:   healthTimeoutSec,
		TargetVersion:   rel.TagName,
		PreviousVersion: u.version,
		ResultPath:      u.resultPath(),
		Binaries: []binarySwap{
			{New: newWlcsim, Live: filepath.Join(u.binDir, "wlcsim"), Bak: filepath.Join(u.binDir, "wlcsim.bak")},
			{New: newConsole, Live: filepath.Join(u.binDir, "wlcsim-console"), Bak: filepath.Join(u.binDir, "wlcsim-console.bak")},
		},
	}
	cfgPath := filepath.Join(u.workDir, "helper.json")
	if err := writeJSONFile(cfgPath, cfg); err != nil {
		u.fail("stage helper config: %v", err)
		return
	}

	u.logf("Verified %s — installing and restarting (brief downtime)…", rel.TagName)
	if err := spawnHelper(cfgPath, updateLogPath); err != nil {
		u.fail("could not launch update helper: %v", err)
		return
	}
	spawned = true
	// The helper will stop this process shortly; leave busy set until then. If it
	// somehow never does, free the busy flag after a generous window so the user
	// can retry.
	time.AfterFunc(4*time.Minute, func() {
		if u.busy.CompareAndSwap(true, false) {
			u.fail("update timed out — service was not restarted")
		}
	})
}

// fetchVerify downloads an asset and checks it against the published checksum.
func (u *Updater) fetchVerify(ctx context.Context, a Asset, dest string, sums map[string]string) error {
	u.logf("Downloading %s", a.Name)
	got, err := downloadFile(ctx, a.BrowserDownloadURL, dest, u.hc)
	if err != nil {
		return fmt.Errorf("download %s: %v", a.Name, err)
	}
	want, ok := sums[a.Name]
	if !ok {
		return fmt.Errorf("no checksum published for %s", a.Name)
	}
	if got != want {
		return fmt.Errorf("checksum mismatch for %s", a.Name)
	}
	u.logf("Verified %s", a.Name)
	return nil
}

// archAssetSuffix maps the running arch to the release asset suffix.
func archAssetSuffix() (string, bool) {
	switch runtime.GOARCH {
	case "amd64":
		return "linux-amd64", true
	case "arm64":
		return "linux-arm64", true
	default:
		return "", false
	}
}

// logf records a progress entry visible in the dashboard's live log panel.
func (u *Updater) logf(format string, args ...any) {
	if u.logs == nil {
		return
	}
	u.logs.Add(accesslog.Entry{
		Type:   "system",
		Source: "update",
		Detail: fmt.Sprintf(format, args...),
	})
}

// fail records an error both to the log panel and the sticky lastErr field.
func (u *Updater) fail(format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	u.logf("Update failed: %s", msg)
	u.mu.Lock()
	u.lastErr = msg
	u.mu.Unlock()
}
