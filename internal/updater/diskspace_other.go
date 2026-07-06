//go:build !linux

package updater

// freeBytes is a no-op on non-Linux platforms. The updater only runs its apply
// path on the Linux appliance; on a developer's machine (darwin) this reports
// effectively unlimited space so the precheck never blocks a dry run.
func freeBytes(path string) (uint64, error) {
	return 1 << 50, nil
}
