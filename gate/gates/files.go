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
		if err := file.Close(); err != nil {
			errs = append(errs, err)
		}
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
	return name == "go.sum" || name == "package-lock.json"
}
