#!/usr/bin/env bash
# =============================================================================
# OpenAgentPlatform — Relay certificate + trust material generator
# =============================================================================
# Mints everything `cmd/relay` needs to run in the compose stack:
#
#   1. relay server cert (WSS + admin listeners; mTLS server side)
#   2. admin operator client cert (role SAN oap:role:relay-admin -> full access)
#   3. two agent client certs (mTLS identity for WSS legs; SANs carry the
#      `oap:tenant:<id>` tenant marker and the agent principal)
#   4. Ed25519 token-signing keypair; trust.yaml carries the public half and
#      the entitlement grants (RELAY-02)
#
# SAN conventions (parsed by internal/relay/ws.go + admin.go):
#   oap:tenant:<id>   tenant binding (WSS + admin)
#   oap:role:relay-admin / oap:role:relay-operator  admin API roles
#   any other SAN/CN  the agent principal
#
# Usage:
#   ./gen-relay-certs.sh                      # defaults below
#   TENANT=demo ./gen-relay-certs.sh
#
# The output dir is gitignored; secrets never land in the repo.
# =============================================================================
set -euo pipefail

CERTS_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/certs"
TRUST_YAML="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/trust.yaml"
DOMAIN="openagentplatform"
TENANT="${TENANT:-default}"
AGENT_A="${AGENT_A:-agent-a}"
AGENT_B="${AGENT_B:-agent-b}"
DAYS=365
CURVE="prime256v1"

mkdir -p "$CERTS_DIR"
cd "$CERTS_DIR"

gen_leaf() { # gen_leaf <name> <cn> <san-block>
  local name="$1" cn="$2" sans="$3"
  openssl ecparam -genkey -name "$CURVE" -noout -out "${name}-key.pem"
  local ext
  ext=$(mktemp)
  cat > "$ext" <<EOF
[req]
distinguished_name = dn
prompt = no

[dn]
CN = ${cn}

[v3]
basicConstraints = CA:FALSE
keyUsage = critical, digitalSignature
extendedKeyUsage = serverAuth, clientAuth
subjectAltName = ${sans}
EOF
  openssl req -new -key "${name}-key.pem" -out "${name}-csr.pem" -config "$ext"
  openssl x509 -req -in "${name}-csr.pem" \
    -CA ca-cert.pem -CAkey ca-key.pem -CAcreateserial \
    -out "${name}-cert.pem" -days "$DAYS" \
    -extfile "$ext" -extensions v3
  rm -f "${name}-csr.pem" "$ext"
  echo "[+] ${name} (CN=${cn})"
}

# ---- CA (separate from the NATS/dev CA: the relay is its own trust domain) ---
if [[ ! -f ca-cert.pem ]]; then
  openssl ecparam -genkey -name "$CURVE" -noout -out ca-key.pem
  openssl req -new -x509 -key ca-key.pem -out ca-cert.pem -days 3650 \
    -subj "/CN=oap relay Root CA" \
    -addext "basicConstraints=critical,CA:TRUE" \
    -addext "keyUsage=critical,keyCertSign,cRLSign"
  echo "[+] CA (oap relay Root CA)"
fi

# ---- relay server cert (serves WSS :7000 and admin) --------------------------
gen_leaf relay-server "relay" \
  "DNS:relay,DNS:localhost,IP:127.0.0.1"

# ---- admin operator client cert (full visibility) ----------------------------
gen_leaf relay-admin "oap:role:relay-admin" \
  "DNS:oap:role:relay-admin"

# ---- agent client certs (tenant-bound WSS legs) ------------------------------
for a in "$AGENT_A" "$AGENT_B"; do
  gen_leaf "agent-${a}" "agent-${a}" \
    "DNS:agent-${a},DNS:oap:tenant:${TENANT}"
done

# ---- Ed25519 platform token key + trust config -------------------------------
if [[ ! -f platform.key ]]; then
  openssl genpkey -algorithm ed25519 -out platform.key 2>/dev/null
fi
# The relay's decodeEd25519Key expects the RAW 32-byte public key (not the
# 44-byte DER SPKI) — strip the 12-byte ASN.1 header.
PUB_B64=$(openssl pkey -in platform.key -pubout -outform DER 2>/dev/null | tail -c 32 | base64 -w0)
cat > "$TRUST_YAML" <<EOF
# Issued-identity trust config (RELAY-02). Version must be 1.
version: 1
# Ed25519 public key (base64 DER SubjectPublicKeyInfo) verifying relay
# admission bearer tokens. The matching private half is platform.key; keep it
# out of the container in production (signing happens on the platform side).
platform_public_key: ${PUB_B64}
entitlements:
  # Demo grant: any agent in the tenant may reach any other agent in it.
  - tenant_id: "${TENANT}"
    source_agent_id: "*"
    target_agent_id: "*"
    action: "relay"
EOF

chmod 600 ./*-key.pem platform.key
chmod 644 ./*-cert.pem ca-cert.pem
chmod 644 "$TRUST_YAML"

echo ""
echo "============================================="
echo "  Relay material generated"
echo "    certs: ${CERTS_DIR}"
echo "    trust: ${TRUST_YAML}"
echo "============================================="
echo "  Mount into the relay container via compose volumes."
echo "  Trust config: trust.yaml (tenant '${TENANT}', agents ${AGENT_A}/${AGENT_B})"
echo "  Keep platform.key OFF any relay host in production."
echo "============================================="
