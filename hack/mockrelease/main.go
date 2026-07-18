// Command mockrelease serves a fake GitHub "latest release" plus its assets so
// the appliance's in-place updater can be exercised end-to-end without
// publishing a real release.
//
// This is a developer tool. It is not part of the wlcsim binary or the OVA
// (release.yml builds only ./cmd/wlcsim; the OVA installs only wlcsim +
// wlcsim-console), so it can never reach a production build.
//
// Usage:
//
//	# Put the "new" binaries you want the appliance to install in a directory:
//	#   wlcsim-linux-<arch>, wlcsim-console-linux-<arch>
//	go run ./hack/mockrelease -dir ./newrelease -tag v9.9.9 -addr :8099
//
// Then build a TEST appliance with the mock hook and point it at this server:
//
//	make ova-arm64 GO_TAGS=updatetest      # (or make build GO_TAGS=updatetest)
//	# on the appliance, give the wlcsim service the env var, e.g. add to
//	# /etc/init.d/wlcsim:   export WLCSIM_UPDATE_API_BASE=http://<this-host>:8099
//
// checksums.txt is generated automatically from the files in -dir, so a
// deliberately-broken wlcsim binary (one that fails to serve :8080) still
// verifies and installs — letting you exercise the auto-rollback path.
package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

func main() {
	dir := flag.String("dir", ".", "directory containing the release asset binaries")
	tag := flag.String("tag", "v9.9.9", "release tag to advertise")
	addr := flag.String("addr", ":8099", "listen address")
	repo := flag.String("repo", "vimukthiD/cisco-wlc-simulator", "owner/name to answer for")
	flag.Parse()

	assetNames := func() ([]string, error) {
		entries, err := os.ReadDir(*dir)
		if err != nil {
			return nil, err
		}
		var names []string
		for _, e := range entries {
			if !e.IsDir() && e.Name() != "checksums.txt" {
				names = append(names, e.Name())
			}
		}
		sort.Strings(names)
		return names, nil
	}

	latestPath := fmt.Sprintf("/repos/%s/releases/latest", *repo)
	http.HandleFunc(latestPath, func(w http.ResponseWriter, r *http.Request) {
		names, err := assetNames()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		base := "http://" + r.Host
		type asset struct {
			Name               string `json:"name"`
			BrowserDownloadURL string `json:"browser_download_url"`
			Size               int64  `json:"size"`
		}
		var assets []asset
		for _, n := range names {
			info, _ := os.Stat(filepath.Join(*dir, n))
			var size int64
			if info != nil {
				size = info.Size()
			}
			assets = append(assets, asset{n, base + "/download/" + n, size})
		}
		assets = append(assets, asset{"checksums.txt", base + "/download/checksums.txt", 0})
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"tag_name":   *tag,
			"html_url":   base + "/releases/" + *tag,
			"prerelease": false,
			"draft":      false,
			"assets":     assets,
		})
		log.Printf("served releases/latest (tag %s) to %s", *tag, r.RemoteAddr)
	})

	http.HandleFunc("/download/", func(w http.ResponseWriter, r *http.Request) {
		name := filepath.Base(strings.TrimPrefix(r.URL.Path, "/download/"))
		if name == "checksums.txt" {
			names, err := assetNames()
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			var b strings.Builder
			for _, n := range names {
				data, err := os.ReadFile(filepath.Join(*dir, n))
				if err != nil {
					http.Error(w, err.Error(), http.StatusInternalServerError)
					return
				}
				sum := sha256.Sum256(data)
				fmt.Fprintf(&b, "%s  %s\n", hex.EncodeToString(sum[:]), n)
			}
			w.Write([]byte(b.String()))
			log.Printf("served checksums.txt to %s", r.RemoteAddr)
			return
		}
		log.Printf("serving asset %s to %s", name, r.RemoteAddr)
		http.ServeFile(w, r, filepath.Join(*dir, name))
	})

	log.Printf("mock release server: dir=%s tag=%s listening on %s", *dir, *tag, *addr)
	log.Printf("point the test appliance at:  WLCSIM_UPDATE_API_BASE=http://<this-host>%s", *addr)
	log.Fatal(http.ListenAndServe(*addr, nil))
}
