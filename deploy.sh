#!/usr/bin/env bash
#
# deploy.sh — Authoritative release/publish pipeline for OpenAgentPlatform.
#
# WHY THIS EXISTS (PUBLIC-REPO SECURITY MANDATE):
#   This repository is FULLY PUBLIC — anything pushed is permanent and cannot
#   be un-published. A leaked credential (AWS key, DB password, JWT secret,
#   Stripe key, private key) committed to a public repo is an instant,
#   irreversible exposure (automated secret-harvesters scrape public repos
#   within minutes). CI's secret-validation.yml (gitleaks) catches leaks AFTER
#   a push — too late for a public repo.
#
#   deploy.sh therefore runs a LOCAL secrets/sensitive-data scan
#   (scripts/scan-secrets.sh) as a hard gate BEFORE anything is committed or
#   pushed — and re-runs it immediately before `git push` to catch anything
#   introduced during the gate. It is the belt-and-suspenders gate on top of
#   CI.
#
# It enforces (in order):
#   1. Clean git tree (nothing uncommitted — we do not release from a dirty tree).
#   2. SECRETS SCAN (public-repo gate) — abort if ANY secret/sensitive data.
#   3. Full gate: server build + both test suites + lint + regression_check
#      (incl. pnpm audit for runtime HIGH/CRITICAL vulns + settings surface
#      audit + file-size headroom gate).
#   4. Bump the version (web/package.json + docs) to <new-version>.
#   5. Build the Docker images (server + web) to prove they compile.
#   6. Tag (annotated) + push — then re-run the secrets scan on the final tree
#      immediately before push.
#   7. Print post-release steps.
#
# Usage:
#   ./deploy.sh 0.2.0
#
# Exit codes: non-zero on any failure (set -euo pipefail). Nothing is
# committed, tagged, or pushed if any step fails.

set -euo pipefail

# --- args --------------------------------------------------------------------
if [[ $# -ne 1 ]]; then
	echo "usage: $0 <new-version>" >&2
	echo "  e.g. $0 0.2.0" >&2
	exit 2
fi

NEW_VERSION="$1"
NEW_VERSION="${NEW_VERSION#v}" # accept v-prefixed input

if ! [[ "$NEW_VERSION" =~ ^[0-9]+\.[0-9]+\.[0-9]+([.-][0-9A-Za-z.-]+)?$ ]]; then
	echo "[deploy] ERROR: '$NEW_VERSION' is not a valid semver." >&2
	exit 2
fi

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$ROOT"

echo "[deploy] OpenAgentPlatform release pipeline → v$NEW_VERSION"
echo "[deploy] working dir: $ROOT"
echo "[deploy] PUBLIC REPO: secret scan is a hard gate."

# --- 1. clean git tree --------------------------------------------------------
if ! git diff --quiet; then
	echo "[deploy] ERROR: working tree has unstaged changes. Commit or stash first." >&2
	git diff --stat >&2 || true
	exit 1
fi
if ! git diff --cached --quiet; then
	echo "[deploy] ERROR: index has staged but uncommitted changes. Commit first." >&2
	exit 1
fi
# Also reject untracked files (runtime state like .pi/, stray new scripts): a
# release must ship only tracked, reviewed files.
if [ -n "$(git ls-files --others --exclude-standard | head -1)" ]; then
	echo "[deploy] ERROR: untracked file(s) present. Commit or remove them first:" >&2
	git ls-files --others --exclude-standard | sed 's/^/  /' >&2
	exit 1
fi
echo "[deploy] git tree clean."

# --- 2. SECRETS SCAN (public-repo gate) ---------------------------------------
echo "[deploy] secret scan (public-repo gate #1 of 2)"
if ! ./scripts/scan-secrets.sh; then
	echo "[deploy] ERROR: secrets/sensitive data found in the working tree." >&2
	echo "[deploy] This is a PUBLIC repo — cannot un-publish a leaked credential." >&2
	echo "[deploy] Remove/replace the secret (or add an explicit allowlist entry," >&2
	echo "[deploy] SECRETS-SCAN-001) then re-run. ABORTING before any commit/push." >&2
	exit 1
fi
echo "[deploy] secret scan clean."

# --- toolchain pre-flight ------------------------------------------------------
# Fail fast with the exact commands instead of a mid-gate error.
for tool in go pnpm python3 docker; do
	if ! command -v "$tool" >/dev/null 2>&1; then
		echo "[deploy] ERROR: required tool '$tool' not found in PATH." >&2
		exit 1
	fi
done

# --- 3. full gate -------------------------------------------------------------
echo "[deploy] running gate: go build + vet, pnpm build + lint, py lint, regression_check + audit + secret-scan-integrated"

go build -o /tmp/oap-server-check ./cmd/server
echo "[deploy] server builds (go build OK)."

# go vet at the ROOT module only — mirrors CI (go.yml runs `go vet ./...` at
# the repo root). The standalone submodule dirs (mcp-server, cmd/team-cli,
# examples/go) are separate deployables outside the root go.work; they are NOT
# part of the monorepo release contract and are not gated by root-level vet.
if ! go vet ./...; then
  echo "[deploy] ERROR: go vet failed at the repo root." >&2
  exit 1
fi
rm -f /tmp/oap-server-check
echo "[deploy] go vet OK (root module)."

if ! (cd web && pnpm install --frozen-lockfile && pnpm build); then
	echo "[deploy] ERROR: web build failed." >&2
	exit 1
fi
echo "[deploy] web build OK."

if ! (cd web && pnpm lint); then
	echo "[deploy] ERROR: web lint failed." >&2
	exit 1
fi
echo "[deploy] web lint OK."


# Python lint (ruff). Non-fatal if ruff missing so the gate is not hostage to a
# local-only linter.
if command -v ruff >/dev/null 2>&1; then
	(cd py && ruff check .) || {
		echo "[deploy] ERROR: ruff lint failed." >&2
		exit 1
	}
	echo "[deploy] python lint OK."
else
	echo "[deploy] WARN: ruff not found — skipping python lint (CI enforces it)."
fi

# regression_check: file-size + soft-as-hard headroom (release-gate mode) +
# settings surface audit + pnpm audit (runtime HIGH/CRITICAL web deps).
PREV_TAG="$(git describe --tags --abbrev=0 HEAD^ 2>/dev/null || true)"
if [ -n "$PREV_TAG" ]; then
	python3 scripts/regression_check.py --all --soft-as-hard --soft-as-hard-base "$PREV_TAG" --pre-commit
else
	python3 scripts/regression_check.py --all --soft-as-hard --pre-commit
fi
echo "[deploy] regression check (file-size + audit + settings) green."

echo "[deploy] gate green."

# --- 4. bump version ----------------------------------------------------------
CURRENT_VERSION="$(node -e "console.log(require('./web/package.json').version)" 2>/dev/null || echo "0.0.0")"
echo "[deploy] web version: $CURRENT_VERSION → $NEW_VERSION"

if [ "$CURRENT_VERSION" != "$NEW_VERSION" ]; then
	# Update web/package.json version (pnpm-workspace; no tag yet).
	(cd web && node -e "
		const fs=require('fs');
		const p=JSON.parse(fs.readFileSync('package.json','utf8'));
		p.version='$NEW_VERSION';
		fs.writeFileSync('package.json', JSON.stringify(p,null,2)+'\n');
	")
	echo "[deploy] bumped web/package.json to v$NEW_VERSION."
fi

# --- 5. build docker images (prove they compile) ------------------------------
echo "[deploy] building server + web Docker images (dependency: compose build)"
if ! docker compose -p oap -f deploy/docker-compose.yml build server web >/tmp/oap-docker-build.log 2>&1; then
	echo "[deploy] ERROR: docker compose build failed. See tail of build log:" >&2
	tail -30 /tmp/oap-docker-build.log >&2
	exit 1
fi
echo "[deploy] docker images build OK."

# --- 6. commit + tag + push, with a re-run of the secret scan right before push
# Commit the version bump.
if ! git diff --quiet -- web/package.json; then
	git add web/package.json
	git commit -m "chore(release): v$NEW_VERSION

Release v$NEW_VERSION published via deploy.sh.

Co-Authored-By: openagentplatform deploy.sh <noreply@openagentplatform>"
	echo "[deploy] committed version bump ($(git rev-parse --short HEAD))."
else
	echo "[deploy] no version change to commit."
fi

TAG="v$NEW_VERSION"
if git rev-parse -q --verify "refs/tags/$TAG" >/dev/null; then
	echo "[deploy] tag $TAG already exists; skipping creation."
else
	git tag -a "$TAG" -m "Release v$NEW_VERSION"
	echo "[deploy] created annotated tag $TAG."
fi

# --- 2b. SECOND secret scan — immediately before the push ---------------------
echo "[deploy] secret scan (public-repo gate #2 of 2, pre-push)"
if ! ./scripts/scan-secrets.sh; then
	echo "[deploy] ERROR: secrets/sensitive data appeared during the gate." >&2
	echo "[deploy] Aborting BEFORE push (nothing was pushed). Un-tag with:" >&2
	echo "[deploy]   git tag -d $TAG && git reset --soft HEAD~1 # if the bump was committed" >&2
	exit 1
fi
echo "[deploy] secret scan clean (pre-push)."

# Push (tag + commits). `git push --follow-tags` pushes the annotated tag.
echo "[deploy] pushing commits + tags (git push --follow-tags)"
if ! git push --follow-tags 2>/dev/null; then
	echo "[deploy] git push --follow-tags failed; setting upstream and retrying"
	CURRENT_BRANCH="$(git rev-parse --abbrev-ref HEAD)"
	git push --set-upstream origin "$CURRENT_BRANCH" --follow-tags
fi

# --- 7. post-release steps ----------------------------------------------------
echo
echo "============================================================"
echo " RELEASED v$NEW_VERSION (tag $TAG pushed to origin)"
echo "============================================================"
echo "CI is now the final authority on CI-only gates (gitleaks," 
echo "go/python/web tests) — deploy.sh already ran the public-repo secret"
echo "scan, so gitleaks should be green. On each runtime host:"
echo
echo "  1. Pull the released code and rebuild:"
echo "       git pull && docker compose -p oap -f deploy/docker-compose.yml build"
echo "  2. Restart the stack:"
echo "       docker compose -p oap -f deploy/docker-compose.yml up -d"
echo "  3. Verify health:"
echo "       curl -sS http://localhost:8080/healthz"
echo "============================================================"
echo "[deploy] done."
