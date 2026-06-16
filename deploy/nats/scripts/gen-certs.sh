#!/usr/bin/env bash
# =============================================================================
# OpenAgentPlatform — Certificate Generation Script
# =============================================================================
# Generates mTLS certificates with SPIFFE URI SANs for NATS server, agents,
# and A2A gateway nodes.
#
# Usage:
#   ./gen-certs.sh                         # generate all certs
#   ./gen-certs.sh --output /path/to/certs # custom output directory
#   ./gen-certs.sh --agent my-agent        # add a specific agent cert
#   ./gen-certs.sh --a2a-peer my-peer      # add an A2A gateway cert
#
# Prerequisites:
#   - OpenSSL 1.1.1+ (or LibreSSL 3.1+)
#
# WARNING: This script generates self-signed certificates suitable for
# development and testing. For production, use a real PKI or SPIRE.
# =============================================================================

set -euo pipefail

# ---- Defaults ----------------------------------------------------------------
CERTS_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../certs" && pwd)"
DOMAIN="openagentplatform"
DAYS_CA=3650       # CA valid for 10 years
DAYS_CERT=365      # Leaf certs valid for 1 year
KEY_SIZE=4096      # RSA key size
EC_CURVE="prime256v1"  # EC curve for agent certs (P-256)

# ---- Argument parsing --------------------------------------------------------
EXTRA_AGENTS=()
EXTRA_A2A=()
while [[ $# -gt 0 ]]; do
  case "$1" in
    --output)   CERTS_DIR="$2"; shift 2 ;;
    --agent)    EXTRA_AGENTS+=("$2"); shift 2 ;;
    --a2a-peer) EXTRA_A2A+=("$2"); shift 2 ;;
    --domain)   DOMAIN="$2"; shift 2 ;;
    --help|-h)
      sed -n '2,/^# ====/{ /^# /s/^# //p }' "$0"
      exit 0
      ;;
    *) echo "Unknown option: $1" >&2; exit 1 ;;
  esac
done

mkdir -p "$CERTS_DIR"
echo "[*] Writing certificates to: $CERTS_DIR"
cd "$CERTS_DIR"

# ---- Helper functions --------------------------------------------------------
gen_ca() {
  echo "[+] Generating Root CA..."
  openssl ecparam -genkey -name "$EC_CURVE" -noout -out ca-key.pem
  openssl req -new -x509 \
    -key ca-key.pem \
    -out ca.pem \
    -days "$DAYS_CA" \
    -subj "/CN=${DOMAIN} Root CA" \
    -addext "basicConstraints=critical,CA:TRUE" \
    -addext "keyUsage=critical,keyCertSign,cRLSign"
  echo "    CA certificate: ca.pem (valid ${DAYS_CA} days)"
}

gen_server_cert() {
  local spiffe_uri="spiffe://${DOMAIN}/ns/oap/server"
  echo "[+] Generating NATS server certificate..."
  echo "    SPIFFE URI SAN: ${spiffe_uri}"

  openssl ecparam -genkey -name "$EC_CURVE" -noout -out server-key.pem

  # Build a temporary openssl config for SANs
  local ext_file
  ext_file=$(mktemp)
  cat > "$ext_file" <<EOF
[req]
distinguished_name = req_dn
req_extensions     = v3_req
prompt             = no

[req_dn]
CN = oap-nats.${DOMAIN}

[v3_req]
basicConstraints = CA:FALSE
keyUsage         = critical, digitalSignature, keyEncipherment
extendedKeyUsage = serverAuth, clientAuth
subjectAltName   = @alt_names

[alt_names]
DNS.1 = oap-nats.${DOMAIN}
DNS.2 = nats
DNS.3 = localhost
URI.1 = ${spiffe_uri}
EOF

  openssl req -new \
    -key server-key.pem \
    -out server-csr.pem \
    -config "$ext_file"

  openssl x509 -req \
    -in server-csr.pem \
    -CA ca.pem \
    -CAkey ca-key.pem \
    -CAcreateserial \
    -out server-cert.pem \
    -days "$DAYS_CERT" \
    -extfile "$ext_file" \
    -extensions v3_req

  rm -f server-csr.pem "$ext_file"
  echo "    Server cert: server-cert.pem (valid ${DAYS_CERT} days)"
}

gen_agent_cert() {
  local name="$1"
  local spiffe_uri="spiffe://${DOMAIN}/agent/${name}"
  echo "[+] Generating agent certificate: ${name}"
  echo "    SPIFFE URI SAN: ${spiffe_uri}"

  openssl ecparam -genkey -name "$EC_CURVE" -noout -out "agent-${name}-key.pem"

  local ext_file
  ext_file=$(mktemp)
  cat > "$ext_file" <<EOF
[req]
distinguished_name = req_dn
req_extensions     = v3_req
prompt             = no

[req_dn]
CN = ${name}.agent.${DOMAIN}

[v3_req]
basicConstraints = CA:FALSE
keyUsage         = critical, digitalSignature, keyEncipherment
extendedKeyUsage = clientAuth
subjectAltName   = @alt_names

[alt_names]
DNS.1 = ${name}.agent.${DOMAIN}
URI.1 = ${spiffe_uri}
EOF

  openssl req -new \
    -key "agent-${name}-key.pem" \
    -out "agent-${name}-csr.pem" \
    -config "$ext_file"

  openssl x509 -req \
    -in "agent-${name}-csr.pem" \
    -CA ca.pem \
    -CAkey ca-key.pem \
    -CAcreateserial \
    -out "agent-${name}-cert.pem" \
    -days "$DAYS_CERT" \
    -extfile "$ext_file" \
    -extensions v3_req

  rm -f "agent-${name}-csr.pem" "$ext_file"
  echo "    Agent cert: agent-${name}-cert.pem (valid ${DAYS_CERT} days)"
}

gen_a2a_cert() {
  local name="$1"
  local spiffe_uri="spiffe://${DOMAIN}/a2a/${name}"
  echo "[+] Generating A2A gateway certificate: ${name}"
  echo "    SPIFFE URI SAN: ${spiffe_uri}"

  openssl ecparam -genkey -name "$EC_CURVE" -noout -out "a2a-${name}-key.pem"

  local ext_file
  ext_file=$(mktemp)
  cat > "$ext_file" <<EOF
[req]
distinguished_name = req_dn
req_extensions     = v3_req
prompt             = no

[req_dn]
CN = ${name}.a2a.${DOMAIN}

[v3_req]
basicConstraints = CA:FALSE
keyUsage         = critical, digitalSignature, keyEncipherment
extendedKeyUsage = clientAuth
subjectAltName   = @alt_names

[alt_names]
DNS.1 = ${name}.a2a.${DOMAIN}
URI.1 = ${spiffe_uri}
EOF

  openssl req -new \
    -key "a2a-${name}-key.pem" \
    -out "a2a-${name}-csr.pem" \
    -config "$ext_file"

  openssl x509 -req \
    -in "a2a-${name}-csr.pem" \
    -CA ca.pem \
    -CAkey ca-key.pem \
    -CAcreateserial \
    -out "a2a-${name}-cert.pem" \
    -days "$DAYS_CERT" \
    -extfile "$ext_file" \
    -extensions v3_req

  rm -f "a2a-${name}-csr.pem" "$ext_file"
  echo "    A2A cert: a2a-${name}-cert.pem (valid ${DAYS_CERT} days)"
}

# ---- Generate all certs ------------------------------------------------------
gen_ca
gen_server_cert

# Default agents for development
for agent in orchestrator planner executor reviewer; do
  gen_agent_cert "$agent"
done

# Extra agents from CLI args
for agent in "${EXTRA_AGENTS[@]:-}"; do
  [[ -z "$agent" ]] && continue
  gen_agent_cert "$agent"
done

# Default A2A peers
gen_a2a_cert "gateway-primary"

# Extra A2A peers from CLI args
for peer in "${EXTRA_A2A[@]:-}"; do
  [[ -z "$peer" ]] && continue
  gen_a2a_cert "$peer"
done

# ---- Set permissions ---------------------------------------------------------
echo ""
echo "[+] Restricting file permissions..."
chmod 600 ./*-key.pem
chmod 644 ./*-cert.pem ca.pem

# ---- Summary -----------------------------------------------------------------
echo ""
echo "============================================="
echo "  Certificate generation complete"
echo "============================================="
echo "  Directory:  $CERTS_DIR"
echo "  CA:         ca.pem / ca-key.pem"
echo "  Server:     server-cert.pem / server-key.pem"
echo ""
echo "  Agent certs:"
ls -1 agent-*-cert.pem 2>/dev/null | sed 's/^/    /'
echo ""
echo "  A2A certs:"
ls -1 a2a-*-cert.pem 2>/dev/null | sed 's/^/    /'
echo ""
echo "  Next steps:"
echo "    1. Copy ca.pem + server-cert/key to deploy/nats/certs/"
echo "    2. Copy agent cert/key pairs to agent deployments"
echo "    3. Run spiffe-mappings.sh to update spiffe-mappings.json"
echo "============================================="
