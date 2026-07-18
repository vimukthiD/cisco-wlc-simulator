//go:build updatetest

// This file is compiled ONLY when the `updatetest` build tag is set (e.g.
// `go build -tags updatetest` / `make ... GO_TAGS=updatetest`). The default
// build, `make build`, `make ova-*`, and the release workflow never set that
// tag, so this override cannot exist in any production binary or OVA — a
// released updater always talks to the real GitHub host.
//
// With the tag, WLCSIM_UPDATE_API_BASE repoints the updater at a mock release
// server (see hack/mockrelease) so the full download/verify/install/restart
// flow can be exercised without publishing a real GitHub release.

package updater

import (
	"log"
	"os"
)

func init() {
	if v := os.Getenv("WLCSIM_UPDATE_API_BASE"); v != "" {
		apiBaseURL = v
		log.Printf("[updater] TEST BUILD: GitHub API base overridden to %s", apiBaseURL)
	}
}
