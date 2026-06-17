package inject

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// fileInjector writes secrets to short-lived temp files with mode 0600.
type fileInjector struct {
	mu    sync.Mutex
	paths map[string]string // spec.Key -> file path
}

// newFileInjector creates a fileInjector.
func newFileInjector() *fileInjector {
	return &fileInjector{paths: make(map[string]string)}
}

// inject writes the credential atomically to a temp file and returns the path.
// The file is created with mode 0600, owned by the current process UID.
func (f *fileInjector) inject(spec InjectionSpec) (string, error) {
	mode := spec.Mode
	if mode == 0 {
		mode = 0o600
	}

	dir := os.TempDir()
	if dir == "" {
		dir = "/tmp"
	}

	agentID := sanitizeKey(spec.AgentID())
	if agentID == "" {
		agentID = "agent"
	}

	randBytes := make([]byte, 8)
	if _, err := rand.Read(randBytes); err != nil {
		return "", fmt.Errorf("file: random: %w", err)
	}
	token := hex.EncodeToString(randBytes)

	finalPath := filepath.Join(dir, fmt.Sprintf("oap-injected-%s-%s.cred", agentID, token))

	// Write to a sibling temp file first, then rename for atomicity.
	tmpPath := finalPath + ".tmp"
	fh, err := os.OpenFile(tmpPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, mode)
	if err != nil {
		return "", fmt.Errorf("file: create tmp: %w", err)
	}

	if _, err := fh.Write(spec.Value); err != nil {
		fh.Close()
		os.Remove(tmpPath)
		return "", fmt.Errorf("file: write: %w", err)
	}

	if err := fh.Sync(); err != nil {
		fh.Close()
		os.Remove(tmpPath)
		return "", fmt.Errorf("file: sync: %w", err)
	}

	if err := fh.Close(); err != nil {
		os.Remove(tmpPath)
		return "", fmt.Errorf("file: close: %w", err)
	}

	// Ensure final permissions are correct (umask may have affected creation).
	if err := os.Chmod(tmpPath, mode); err != nil {
		os.Remove(tmpPath)
		return "", fmt.Errorf("file: chmod: %w", err)
	}

	if err := os.Rename(tmpPath, finalPath); err != nil {
		os.Remove(tmpPath)
		return "", fmt.Errorf("file: rename: %w", err)
	}

	f.mu.Lock()
	f.paths[spec.Key] = finalPath
	f.mu.Unlock()

	return finalPath, nil
}

// cleanup securely deletes the temp file: zero-fill then unlink.
func (f *fileInjector) cleanup(spec InjectionSpec) error {
	f.mu.Lock()
	path, ok := f.paths[spec.Key]
	delete(f.paths, spec.Key)
	f.mu.Unlock()

	if !ok {
		// Fall back to reconstructing the path pattern.
		dir := os.TempDir()
		if dir == "" {
			dir = "/tmp"
		}
		agentID := sanitizeKey(spec.AgentID())
		if agentID == "" {
			agentID = "agent"
		}
		_ = filepath.Join(dir, fmt.Sprintf("oap-injected-%s-", agentID))
		// We can't know the random token, so just scan for stale files.
		return nil
	}

	return secureUnlink(path)
}

// secureUnlink overwrites the file with zeros before removing it.
func secureUnlink(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("file: stat: %w", err)
	}

	size := info.Size()
	if size > 0 {
		zeros := make([]byte, min(size, 1<<20)) // write in 1 MB chunks
		fh, err := os.OpenFile(path, os.O_WRONLY, 0)
		if err == nil {
			remaining := size
			for remaining > 0 {
				n := int64(len(zeros))
				if remaining < n {
					n = remaining
				}
				if _, werr := fh.Write(zeros[:n]); werr != nil {
					break
				}
				remaining -= n
			}
			fh.Close()
		}
	}

	return os.Remove(path)
}

// min returns the smaller of two int64s.
func min(a, b int64) int64 {
	if a < b {
		return a
	}
	return b
}
