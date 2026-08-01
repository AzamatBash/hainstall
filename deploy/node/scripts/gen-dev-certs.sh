#!/usr/bin/env bash
# Generate a self-signed TLS cert for local HAProxy testing.
# Output: deploy/node/certs/site.pem (fullchain + key combined)
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
CERT_DIR="${ROOT}/certs"
DOMAIN="${HAPANEL_DOMAIN:-localhost}"
DAYS="${CERT_DAYS:-825}"

mkdir -p "${CERT_DIR}"

KEY="${CERT_DIR}/privkey.pem"
CRT="${CERT_DIR}/fullchain.pem"
SITE="${CERT_DIR}/site.pem"
CNF="${CERT_DIR}/openssl.cnf"

cat > "${CNF}" <<EOF
[req]
default_bits = 2048
prompt = no
default_md = sha256
distinguished_name = dn
x509_extensions = v3_req

[dn]
CN = ${DOMAIN}

[v3_req]
subjectAltName = @alt_names
basicConstraints = CA:FALSE
keyUsage = digitalSignature, keyEncipherment
extendedKeyUsage = serverAuth

[alt_names]
DNS.1 = ${DOMAIN}
DNS.2 = localhost
IP.1 = 127.0.0.1
EOF

openssl req -x509 -newkey rsa:2048 -nodes \
  -keyout "${KEY}" \
  -out "${CRT}" \
  -days "${DAYS}" \
  -config "${CNF}"

cat "${CRT}" "${KEY}" > "${SITE}"
chmod 644 "${CRT}" "${SITE}"
chmod 600 "${KEY}"

echo "Wrote ${SITE} (CN=${DOMAIN}, days=${DAYS})"
