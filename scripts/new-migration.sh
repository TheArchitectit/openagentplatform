#!/usr/bin/env bash
# Create the next schema migration pair in the canonical embedded set.
#
#   ./scripts/new-migration.sh add_backup_jobs
#
# Writes internal/db/migrations/NNN_<slug>.up.sql with a real header and a
# matching no-op .down.sql (beta is roll-forward only — downs are intentional
# empty comments; a fresh database is rebuilt by destroying the compose
# volume). The version number is the next contiguous integer; internal/db's
# sanity test enforces contiguity and up/down pairing.
set -euo pipefail

slug="${1:?usage: new-migration.sh <snake_case_name>}"
[[ "$slug" =~ ^[a-z0-9_]+$ ]] || { echo "name must be lowercase snake_case: $slug" >&2; exit 1; }

dir="$(cd "$(dirname "$0")/.." && pwd)/internal/db/migrations"
max=0
for f in "$dir"/*.up.sql; do
  n="$(basename "$f" | cut -d_ -f1)"
  (( 10#$n > max )) && max=$((10#$n))
done
next=$(printf '%03d' $((max + 1)))

up="$dir/${next}_${slug}.up.sql"
down="$dir/${next}_${slug}.down.sql"
[[ -e "$up" ]] && { echo "$up already exists" >&2; exit 1; }

cat > "$up" <<EOF
-- Migration ${next}: ${slug}
-- Canonical platform schema. Applied automatically at server boot
-- (OAP_AUTO_MIGRATE=true) or via \`go run ./cmd/migrate up\`.
-- Write forward-only SQL here; never edit an already-applied migration.

EOF

cat > "$down" <<EOF
-- Rollback for ${next}_${slug}: deliberate no-op.
-- Beta policy is roll-forward (see internal/db/migrate.go header).

EOF

echo "created:"
echo "  $up"
echo "  $down"
echo "edit the .up.sql, then: go run ./cmd/migrate up"
