package gates

import (
	"bufio"
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// binaryExtensions lists file extensions that should be skipped by scan gates
// to avoid false positives from compiled/binary content.
var binaryExtensions = map[string]bool{
	".png": true, ".jpg": true, ".jpeg": true, ".gif": true, ".ico": true,
	".svg": true, ".woff": true, ".woff2": true, ".ttf": true, ".eot": true,
	".pdf": true, ".zip": true, ".gz": true, ".tar": true, ".bz2": true,
	".exe": true, ".dll": true, ".so": true, ".dylib": true, ".wasm": true,
	".pyc": true, ".pyo": true, ".class": true, ".o": true, ".a": true,
}

// isBinaryFile checks the first 512 bytes for null bytes, which is a
// reliable heuristic for detecting binary content.
func isBinaryFile(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false // let the caller handle the open error
	}
	defer f.Close()

	buf := make([]byte, 512)
	n, err := f.Read(buf)
	if err != nil && n == 0 {
		return false
	}
	for _, b := range buf[:n] {
		if b == 0 {
			return true
		}
	}
	return false
}

type sourceLine struct {
	path   string
	number int
	text   string
}

func walkLines(ctx context.Context, paths []string, visit func(sourceLine)) error {
	files, err := expandPaths(paths)
	if err != nil {
		return err
	}
	var errs []error
	for _, path := range files {
		if err := ctx.Err(); err != nil {
			return err
		}
		file, err := os.Open(path)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		scanner := bufio.NewScanner(file)
		scanner.Buffer(make([]byte, 64*1024), 1024*1024)
		line := 0
		for scanner.Scan() {
			line++
			visit(sourceLine{path: path, number: line, text: scanner.Text()})
		}
		if err := scanner.Err(); err != nil {
			errs = append(errs, err)
		}
		file.Close()
	}
	return errors.Join(errs...)
}

func expandPaths(paths []string) ([]string, error) {
	seen := make(map[string]struct{})
	var files []string
	var errs []error
	for _, root := range paths {
		info, err := os.Stat(root)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		if !info.IsDir() {
			if !skipFile(root) {
				if _, ok := seen[root]; !ok {
					seen[root] = struct{}{}
					files = append(files, root)
				}
			}
			continue
		}
		err = filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if entry.IsDir() {
				if path != root && strings.HasPrefix(entry.Name(), ".") {
					return filepath.SkipDir
				}
				return nil
			}
			if skipFile(path) {
				return nil
			}
			if _, ok := seen[path]; !ok {
				seen[path] = struct{}{}
				files = append(files, path)
			}
			return nil
		})
		if err != nil {
			errs = append(errs, err)
		}
	}
	sort.Strings(files)
	return files, errors.Join(errs...)
}

func skipFile(path string) bool {
	name := filepath.Base(path)
	if strings.HasSuffix(name, "_test.go") || strings.HasSuffix(name, ".min.js") {
		return true
	}
	if name == "go.sum" || name == "package-lock.json" {
		return true
	}
	// Skip known binary extensions.
	ext := strings.ToLower(filepath.Ext(name))
	if binaryExtensions[ext] {
		return true
	}
	// Detect binary content via null-byte heuristic.
	if isBinaryFile(path) {
		return true
	}
	return false
}
