#!/usr/bin/env bash
set -euo pipefail

echo "Code-Switch-R verification (Linux)"
echo "Time: $(date -Iseconds)"
echo

HOSTS_FILE="/etc/hosts"
MARKER_START="# === Code-Switch MITM Start ==="
MARKER_END="# === Code-Switch MITM End ==="
CERT_FILE="/usr/local/share/ca-certificates/code-switch-mitm.crt"
LOG_DIR="${HOME}/.code-switch/logs"
DB_PATH="${HOME}/.code-switch/app.db"
CERT_DIR="${HOME}/.code-switch/certs"

echo "[1] Hosts"
if [[ -f "${HOSTS_FILE}" ]]; then
  if grep -qF "${MARKER_START}" "${HOSTS_FILE}"; then
    echo "  - marker: FOUND (${MARKER_START} ... ${MARKER_END})"
  else
    echo "  - marker: NOT FOUND"
  fi
else
  echo "  - hosts file not found: ${HOSTS_FILE}"
fi
echo

echo "[2] Root CA"
if [[ -f "${CERT_FILE}" ]]; then
  echo "  - installed: YES (${CERT_FILE})"
else
  echo "  - installed: NO (${CERT_FILE})"
fi
echo

echo "[3] Port 443"
if command -v ss >/dev/null 2>&1; then
  ss -ltnp | awk 'NR==1 || $4 ~ /:443$/ {print}' || true
else
  echo "  - ss not found; try netstat"
  if command -v netstat >/dev/null 2>&1; then
    netstat -ltnp 2>/dev/null | grep -E '(^Proto|:443 )' || true
  else
    echo "  - netstat not found; skip"
  fi
fi
echo

echo "[4] App data"
if [[ -f "${DB_PATH}" ]]; then
  echo "  - db: ${DB_PATH} (exists)"
else
  echo "  - db: ${DB_PATH} (missing)"
fi

if [[ -d "${CERT_DIR}" ]]; then
  echo "  - cert dir: ${CERT_DIR} (exists)"
  ls -la "${CERT_DIR}" || true
else
  echo "  - cert dir: ${CERT_DIR} (missing)"
fi

if [[ -d "${LOG_DIR}" ]]; then
  echo "  - log dir: ${LOG_DIR} (exists)"
  ls -la "${LOG_DIR}" || true
else
  echo "  - log dir: ${LOG_DIR} (missing)"
fi

