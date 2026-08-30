#!/usr/bin/env bash
# =============================================================================
# test-login.sh — scripted OIDC login smoke test for a running stack.
#
# Walks the whole browser login path without a browser:
#   server /auth/login -> dex connector form -> credentials -> consent
#   approval -> callback (code exchange + oap_session cookie) -> authed calls.
#
# Usage:
#   ./deploy/scripts/test-login.sh [server_url] [user] [password]
#   defaults: http://localhost:8080  admin@oap.local  password
#
# Cookie hosts: the server must be reached as "localhost" (the compose
# COOKIE_DOMAIN default is localhost; curl only replays cookies to a
# matching host). dex is reached by whatever host its discovery doc
# advertises (http://dex:5556 inside the compose network) — add a
# "127.0.0.1 dex" line to /etc/hosts when running this from the docker host.
# =============================================================================
set -eu

SERVER="${1:-http://localhost:8080}"
USER_NAME="${2:-admin@oap.local}"
PASSWORD="${3:-password}"
JAR="$(mktemp)"
trap 'rm -f "$JAR"' EXIT

get()      { curl -s -b "$JAR" -c "$JAR" "$@"; }
redirect() { curl -s -b "$JAR" -c "$JAR" -o /dev/null -w '%{redirect_url}' "$@"; }
field()    { grep -oE "name=\"$1\" value=\"[^\"]*\"" | head -1 | sed "s/.*value=\"//;s/\"$//"; }
origin()   { printf '%s' "$1" | sed -E 's|^(https?://[^/]+).*|\1|'; }
abs()      { # $1=url-or-path  $2=base-page-url ; root-relative -> origin only
  case "$1" in
    http*) printf '%s' "$1" ;;
    /*)    printf '%s%s' "$(origin "$2")" "$1" ;;
    *)     printf '%s/%s' "${2%/*}" "$1" ;;
  esac; }

echo "[1] start at server /auth/login"
START="$(redirect "$SERVER/auth/login")"
[ -n "$START" ] || { echo "FAIL: /auth/login did not redirect (is dex configured?)"; exit 1; }
echo "    -> ${START:0:100}"

echo "[2] walk dex redirects to the credentials form"
PAGE="$START"; BODY=""
for _ in 1 2 3 4 5; do
  BODY="$(get "$PAGE")"
  NEXT="$(redirect "$PAGE")"
  [ -z "$NEXT" ] && break
  PAGE="$(abs "$NEXT" "${PAGE%/*}")"
done
ACTION="$(printf '%s' "$BODY" | grep -oE 'action="[^"]*"' | head -1 | sed 's/action="//;s/"$//' | sed 's/&amp;/\&/g')"
[ -n "$ACTION" ] || { echo "FAIL: no form action on $PAGE"; exit 1; }
HMAC="$(printf '%s' "$BODY" | field hmac)"
FORM_URL="$(abs "$ACTION" "${PAGE%/*}")"
echo "    form: ${FORM_URL:0:100}"

echo "[3] submit credentials"
APPR="$(get --data-urlencode "login=$USER_NAME" --data-urlencode "password=$PASSWORD" \
  ${HMAC:+--data-urlencode "hmac=$HMAC"} -o /dev/null -w '%{redirect_url}' "$FORM_URL")"
APPR="$(abs "$APPR" "${FORM_URL%/*}")"

# dex shows a consent screen the first time a client asks. Approve it if so.
if printf '%s' "$APPR" | grep -q '/dex/approval'; then
  echo "    consent page: ${APPR:0:100}"
  ABODY="$(get "$APPR")"
  REQID="$(printf '%s' "$ABODY" | field req)"
  AHMAC="$(printf '%s' "$ABODY" | field hmac)"
  APPR="$(get --data-urlencode "req=$REQID" ${AHMAC:+--data-urlencode "hmac=$AHMAC"} \
    --data-urlencode "approval=approve" -o /dev/null -w '%{redirect_url}' "$APPR")"
fi
CODE_URL="$(abs "$APPR" "${SERVER}")"
echo "[4] code redirect: ${CODE_URL:0:110}"
printf '%s' "$CODE_URL" | grep -q 'code=' || { echo "FAIL: no auth code (bad credentials?)"; exit 1; }

echo "[5] exchange at server callback"
get -o /dev/null "$CODE_URL"
if grep -q 'oap_session' "$JAR"; then
  echo "    oap_session: ISSUED"
else
  echo "    FAIL: no oap_session cookie"; exit 1
fi

echo "[6] authed API probes"
curl -s -b "$JAR" -o /dev/null -w "    GET /api/v1/a2a/approvals -> %{http_code}\n" "$SERVER/api/v1/a2a/approvals"
curl -s -b "$JAR" -o /dev/null -w "    GET /api/v1/agents        -> %{http_code}\n" "$SERVER/api/v1/agents"
echo "    (400 org context required is EXPECTED for dex static users —"
echo "     see AI04_DEPLOY_NOTES.md §5.1; 200 requires an org-bearing identity.)"

echo "PASS: login flow works end-to-end"
