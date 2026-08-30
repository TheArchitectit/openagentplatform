package db

import (
	"io/fs"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// TestEmbeddedMigrationsSanity locks down the invariants of the embedded
// canonical schema set without needing a database: files come in up/down
// pairs, versions are unique and contiguous from 001, and names follow the
// golang-migrate convention. The server boots fresh databases from this set,
// so a malformed file here is a deployment failure later.
func TestEmbeddedMigrationsSanity(t *testing.T) {
	names, err := fs.Glob(migrationsFS, "migrations/*.sql")
	if err != nil {
		t.Fatalf("glob: %v", err)
	}
	if len(names) == 0 {
		t.Fatal("embedded migrations set is empty — did the //go:embed path break?")
	}

	versionRe := regexp.MustCompile(`^(\d{3})_([a-z0-9_]+)\.(up|down)\.sql$`)
	seen := map[int]string{}
	downSeen := map[int]bool{}
	for _, name := range names {
		base := strings.TrimPrefix(name, "migrations/")
		m := versionRe.FindStringSubmatch(base)
		if m == nil {
			t.Errorf("file %q does not match NNN_name.{up,down}.sql", base)
			continue
		}
		v, _ := strconv.Atoi(m[1])
		if m[3] == "up" {
			if prev, dup := seen[v]; dup {
				t.Errorf("duplicate version %03d: %q vs %q", v, prev, base)
			}
			seen[v] = m[2]
		} else {
			downSeen[v] = true
		}
	}

	if len(seen) == 0 {
		t.Fatal("no up migrations found")
	}
	maxV := 0
	for v, slug := range seen {
		if v > maxV {
			maxV = v
		}
		if !downSeen[v] {
			t.Errorf("migration %03d_%s has no .down.sql (roll-forward no-ops still require the file)", v, slug)
		}
	}
	for v := 1; v <= maxV; v++ {
		if _, ok := seen[v]; !ok {
			t.Errorf("version gap: no migration %03d (status reporting assumes contiguous numbering)", v)
		}
	}
}
