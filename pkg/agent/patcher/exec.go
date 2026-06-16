package patcher

import "os/exec"

// execPath wraps exec.LookPath so the installer package doesn't need
// its own import. It's a tiny indirection that keeps the package
// surface tidy.
func execPath(name string) (string, error) {
	return exec.LookPath(name)
}
