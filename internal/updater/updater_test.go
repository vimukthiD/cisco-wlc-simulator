package updater

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

// withAPIBase points the (package-global) GitHub API base at a test server for
// the duration of a test. Safe because tests run serially within the package.
func withAPIBase(t *testing.T, url string) {
	t.Helper()
	old := apiBaseURL
	apiBaseURL = url
	t.Cleanup(func() { apiBaseURL = old })
}

func TestFetchLatestRelease(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/owner/name/releases/latest" {
			http.NotFound(w, r)
			return
		}
		fmt.Fprint(w, `{"tag_name":"v1.2.3","html_url":"http://x/rel","assets":[{"name":"wlcsim-linux-amd64","browser_download_url":"http://x/a","size":10}]}`)
	}))
	defer ts.Close()
	withAPIBase(t, ts.URL)

	rel, err := fetchLatestRelease(context.Background(), "owner/name", ts.Client())
	if err != nil {
		t.Fatalf("fetchLatestRelease: %v", err)
	}
	if rel.TagName != "v1.2.3" {
		t.Errorf("tag = %q, want v1.2.3", rel.TagName)
	}
	if a, ok := rel.asset("wlcsim-linux-amd64"); !ok || a.BrowserDownloadURL != "http://x/a" {
		t.Errorf("asset lookup failed: %+v ok=%v", a, ok)
	}
}

func TestFetchLatestReleaseErrors(t *testing.T) {
	for _, code := range []int{http.StatusForbidden, http.StatusNotFound, http.StatusInternalServerError} {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(code)
		}))
		withAPIBase(t, ts.URL)
		_, err := fetchLatestRelease(context.Background(), "o/n", ts.Client())
		ts.Close()
		if err == nil {
			t.Errorf("expected an error for HTTP %d", code)
		}
	}
}

func TestFetchChecksums(t *testing.T) {
	// sha256sum output: "<hash>  <name>" (text mode) and "<hash> *<name>" (binary mode).
	body := "abc123  wlcsim-linux-amd64\ndef456 *wlcsim-console-linux-amd64\nnot a checksum line\n"
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, body)
	}))
	defer ts.Close()

	sums, err := fetchChecksums(context.Background(), ts.URL, ts.Client())
	if err != nil {
		t.Fatalf("fetchChecksums: %v", err)
	}
	if sums["wlcsim-linux-amd64"] != "abc123" {
		t.Errorf("amd64 = %q, want abc123", sums["wlcsim-linux-amd64"])
	}
	if sums["wlcsim-console-linux-amd64"] != "def456" {
		t.Errorf("console (binary-mode '*' should be stripped) = %q, want def456", sums["wlcsim-console-linux-amd64"])
	}
}

func TestDownloadAndVerify(t *testing.T) {
	content := []byte("hello wlcsim binary")
	sum := sha256.Sum256(content)
	wantHex := hex.EncodeToString(sum[:])
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(content)
	}))
	defer ts.Close()

	dest := filepath.Join(t.TempDir(), "wlcsim.new")
	got, err := downloadFile(context.Background(), ts.URL, dest, ts.Client())
	if err != nil {
		t.Fatalf("downloadFile: %v", err)
	}
	if got != wantHex {
		t.Errorf("sha256 = %s, want %s", got, wantHex)
	}
	if data, _ := os.ReadFile(dest); string(data) != string(content) {
		t.Errorf("downloaded file content mismatch")
	}

	// fetchVerify accepts a matching checksum and rejects a mismatch.
	u := &Updater{hc: ts.Client()}
	asset := Asset{Name: "wlcsim-linux-amd64", BrowserDownloadURL: ts.URL}
	if err := u.fetchVerify(context.Background(), asset, filepath.Join(t.TempDir(), "ok"),
		map[string]string{"wlcsim-linux-amd64": wantHex}); err != nil {
		t.Errorf("fetchVerify with correct checksum: %v", err)
	}
	if err := u.fetchVerify(context.Background(), asset, filepath.Join(t.TempDir(), "bad"),
		map[string]string{"wlcsim-linux-amd64": "deadbeef"}); err == nil {
		t.Errorf("fetchVerify should reject a checksum mismatch")
	}
}

func TestCheckLatestAndStatus(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"tag_name":"v9.9.9","html_url":"http://x/r","assets":[]}`)
	}))
	defer ts.Close()
	withAPIBase(t, ts.URL)

	u := New("v0.0.1", nil, Options{HTTPClient: ts.Client()})
	u.appliance = true // simulate the appliance so update_available is computed

	if _, err := u.CheckLatest(context.Background()); err != nil {
		t.Fatalf("CheckLatest: %v", err)
	}
	st := u.Status()
	if st.LatestVersion != "v9.9.9" || !st.UpdateAvailable {
		t.Errorf("status = %+v, want latest v9.9.9 and update_available", st)
	}
}
