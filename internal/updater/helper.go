package updater

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// detachedProcAttr starts the helper in its own session so it survives the
// death of the simulator process it is about to restart.
func detachedProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{Setsid: true}
}

// binarySwap describes one binary to install: the freshly-downloaded staged
// file, the live path it replaces, and where the previous live binary is
// backed up for rollback.
type binarySwap struct {
	New  string `json:"new"`
	Live string `json:"live"`
	Bak  string `json:"bak"`
}

// helperConfig is the JSON handed to the detached helper process. It is written
// by the running simulator and read back by the same binary re-executed with
// -update-helper.
type helperConfig struct {
	ServiceName     string       `json:"service_name"`
	HealthURL       string       `json:"health_url"`
	HealthTimeout   int          `json:"health_timeout_secs"`
	TargetVersion   string       `json:"target_version"`
	PreviousVersion string       `json:"previous_version"`
	ResultPath      string       `json:"result_path"`
	Binaries        []binarySwap `json:"binaries"`
}

// Result records the outcome of an update attempt. It is written by the helper
// after the restart and surfaced through the dashboard so the post-restart page
// can report success or an automatic rollback.
type Result struct {
	Phase           string    `json:"phase"` // "done" | "rolled_back"
	TargetVersion   string    `json:"target_version"`
	PreviousVersion string    `json:"previous_version"`
	Message         string    `json:"message"`
	Timestamp       time.Time `json:"timestamp"`
}

// spawnHelper re-executes the *current* (pre-swap, known-good) binary as a
// detached process that performs the stop/swap/start/health-check/rollback
// dance. Using /proc/self/exe pins the running inode, so the helper keeps
// executing the old, trusted code even after the on-disk binary is replaced.
func spawnHelper(cfgPath, logPath string) error {
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		logFile = nil // fall back to discarding helper output
	}

	cmd := exec.Command("/proc/self/exe", "-update-helper", cfgPath)
	cmd.SysProcAttr = detachedProcAttr()
	if logFile != nil {
		cmd.Stdout = logFile
		cmd.Stderr = logFile
	}
	if err := cmd.Start(); err != nil {
		if logFile != nil {
			logFile.Close()
		}
		return err
	}
	// The helper outlives us; release it and let it own the log file fd.
	_ = cmd.Process.Release()
	return nil
}

// RunHelper is the entry point for the detached `-update-helper` invocation. It
// never returns to normal operation — it installs the update (or rolls back)
// and exits. All output goes to the log file wired up by spawnHelper.
func RunHelper(cfgPath string) {
	log.SetFlags(log.LstdFlags)
	log.Printf("[update-helper] starting, config=%s", cfgPath)

	data, err := os.ReadFile(cfgPath)
	if err != nil {
		log.Printf("[update-helper] cannot read config: %v", err)
		os.Exit(1)
	}
	var cfg helperConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		log.Printf("[update-helper] cannot parse config: %v", err)
		os.Exit(1)
	}

	res := runSequence(cfg)
	res.Timestamp = time.Now()
	if err := writeJSONFile(cfg.ResultPath, res); err != nil {
		log.Printf("[update-helper] failed to write result: %v", err)
	}
	log.Printf("[update-helper] done: %s — %s", res.Phase, res.Message)
	// Best-effort cleanup of the config handoff file.
	os.Remove(cfgPath)
	os.Exit(0)
}

// runSequence performs the actual cut-over. It is deliberately linear and
// defensive: it confirms the old process is really gone before touching any
// binary, and any failure to bring the new binaries up triggers a rollback
// whose reported outcome is grounded in a real post-recovery health check.
func runSequence(cfg helperConfig) Result {
	svc := cfg.ServiceName
	timeout := time.Duration(cfg.HealthTimeout) * time.Second

	log.Printf("[update-helper] stopping %s", svc)
	if err := runService(svc, "stop"); err != nil {
		log.Printf("[update-helper] stop returned error (verifying anyway): %v", err)
	}
	// Confirm the old process actually stopped before swapping anything. If it
	// won't stop (stale pidfile, or running outside OpenRC), abort with nothing
	// installed — the service is still up on the current binaries.
	if !waitStopped(cfg.HealthURL, timeout) {
		log.Printf("[update-helper] service still responding after stop; aborting")
		startAndLog(svc)
		return Result{Phase: "failed", TargetVersion: cfg.TargetVersion, PreviousVersion: cfg.PreviousVersion,
			Message: "service did not stop; update aborted (nothing changed)"}
	}

	// Install: back up each live binary, then move the staged binary in.
	var installed []binarySwap
	for _, b := range cfg.Binaries {
		if err := os.Rename(b.Live, b.Bak); err != nil && !os.IsNotExist(err) {
			log.Printf("[update-helper] backup %s failed: %v", b.Live, err)
			// b.Live is untouched; roll back only the previously installed ones.
			return recoverFailure(cfg, installed, timeout,
				fmt.Sprintf("could not back up %s: %v", filepath.Base(b.Live), err))
		}
		if err := os.Rename(b.New, b.Live); err != nil {
			log.Printf("[update-helper] install %s failed: %v", b.Live, err)
			// b was backed up but not replaced; restore it along with the rest.
			return recoverFailure(cfg, append(installed, b), timeout,
				fmt.Sprintf("could not install %s: %v", filepath.Base(b.Live), err))
		}
		os.Chmod(b.Live, 0o755)
		installed = append(installed, b)
		log.Printf("[update-helper] installed %s", b.Live)
	}

	log.Printf("[update-helper] starting %s", svc)
	if err := runService(svc, "start"); err != nil {
		log.Printf("[update-helper] start failed: %v", err)
	}

	if waitHealthy(cfg.HealthURL, timeout) {
		log.Printf("[update-helper] health check passed")
		restartConsole() // pick up the new console binary (respawned by inittab)
		return Result{Phase: "done", TargetVersion: cfg.TargetVersion,
			PreviousVersion: cfg.PreviousVersion,
			Message:         fmt.Sprintf("Updated to %s", cfg.TargetVersion)}
	}

	log.Printf("[update-helper] health check FAILED; rolling back")
	return recoverFailure(cfg, installed, timeout,
		fmt.Sprintf("Update to %s failed its health check", cfg.TargetVersion))
}

// recoverFailure restores the given binaries from their backups, restarts the
// service, and reports an outcome grounded in whether recovery actually brought
// the service back — never an optimistic "rolled_back". toRestore holds every
// binary whose live path may have been moved (installed ones plus, for an
// install failure, the one that was backed up but not replaced).
func recoverFailure(cfg helperConfig, toRestore []binarySwap, timeout time.Duration, reason string) Result {
	if err := runService(cfg.ServiceName, "stop"); err != nil {
		log.Printf("[update-helper] stop-for-rollback returned error (continuing): %v", err)
	}
	failedRestores := rollback(toRestore)
	startAndLog(cfg.ServiceName)

	phase := "rolled_back"
	msg := reason + fmt.Sprintf("; rolled back to %s", cfg.PreviousVersion)
	if failedRestores > 0 {
		phase = "failed"
		msg = reason + fmt.Sprintf("; ROLLBACK INCOMPLETE — %d binary(ies) could not be restored, check %s",
			failedRestores, updateLogPath)
	}
	if waitHealthy(cfg.HealthURL, timeout) {
		log.Printf("[update-helper] service healthy after recovery")
	} else {
		log.Printf("[update-helper] WARNING: service NOT responding after recovery")
		phase = "failed"
		msg += " (service not responding — check " + updateLogPath + ")"
	}
	return Result{Phase: phase, TargetVersion: cfg.TargetVersion,
		PreviousVersion: cfg.PreviousVersion, Message: msg}
}

// rollback restores each binary from its backup, returning how many restores
// failed (0 means a clean rollback).
func rollback(toRestore []binarySwap) int {
	failed := 0
	for _, b := range toRestore {
		if err := os.Rename(b.Bak, b.Live); err != nil {
			log.Printf("[update-helper] rollback of %s failed: %v", b.Live, err)
			failed++
		} else {
			os.Chmod(b.Live, 0o755)
			log.Printf("[update-helper] restored %s", b.Live)
		}
	}
	return failed
}

func startAndLog(svc string) {
	if err := runService(svc, "start"); err != nil {
		log.Printf("[update-helper] start failed: %v", err)
	}
}

// restartConsole best-effort signals the wlcsim-console process (respawned by
// /etc/inittab, not OpenRC) to exit so init relaunches it on the new binary.
// Failure is non-fatal: the console reads live data from the updated dashboard
// regardless, and picks up new code on its next respawn or a reboot.
func restartConsole() {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return
	}
	self := os.Getpid()
	for _, e := range entries {
		pid, err := strconv.Atoi(e.Name())
		if err != nil || pid == self {
			continue
		}
		comm, err := os.ReadFile(filepath.Join("/proc", e.Name(), "comm"))
		if err != nil {
			continue
		}
		if strings.TrimSpace(string(comm)) != "wlcsim-console" {
			continue
		}
		if p, err := os.FindProcess(pid); err == nil {
			p.Signal(syscall.SIGTERM)
			log.Printf("[update-helper] signaled wlcsim-console (pid %d) to restart", pid)
		}
	}
}

// waitStopped polls url until it stops returning HTTP 200 (the old process is
// gone) or the timeout elapses. Returns true once the service is observed down.
func waitStopped(url string, timeout time.Duration) bool {
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	client := &http.Client{Timeout: 2 * time.Second}
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		resp, err := client.Get(url)
		if err != nil {
			return true // connection refused / no response → stopped
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return true
		}
		time.Sleep(1 * time.Second)
	}
	return false
}

// runService invokes the OpenRC service manager.
func runService(name, action string) error {
	bin := "rc-service"
	if _, err := exec.LookPath(bin); err != nil {
		bin = "/sbin/rc-service" // Alpine default location
	}
	out, err := exec.Command(bin, name, action).CombinedOutput()
	if len(out) > 0 {
		log.Printf("[update-helper] rc-service %s %s: %s", name, action, out)
	}
	return err
}

// waitHealthy polls url until it returns HTTP 200 or the timeout elapses.
func waitHealthy(url string, timeout time.Duration) bool {
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	client := &http.Client{Timeout: 3 * time.Second}
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		resp, err := client.Get(url)
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return true
			}
		}
		time.Sleep(2 * time.Second)
	}
	return false
}

// writeJSONFile atomically writes v as indented JSON (temp file + rename).
func writeJSONFile(path string, v any) error {
	if dir := filepath.Dir(path); dir != "" {
		os.MkdirAll(dir, 0o755)
	}
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// readResult reads a previously written update outcome, if present.
func readResult(path string) (*Result, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, false
	}
	var res Result
	if err := json.Unmarshal(data, &res); err != nil {
		return nil, false
	}
	return &res, true
}
