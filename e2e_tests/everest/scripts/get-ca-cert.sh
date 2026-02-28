#!/usr/bin/env bash

BEARER_TOKEN="$1"
if [[ "$BEARER_TOKEN" == "" ]]; then
  echo "You must provide a bearer token"
  exit 1
fi

BEARER_TOKEN=${BEARER_TOKEN#"Bearer "}

script_dir=$( cd -- "$( dirname -- "${BASH_SOURCE[0]}" )" &> /dev/null && pwd )
cert_dir="${script_dir}"/../config/certificates

# Fetch and decode CPO CA certs
# Try PKCS7 DER format first, then fall back to PEM
fetch_certs() {
  local url="$1"
  local raw
  raw=$(curl -sf "$url" \
    -H 'Accept: application/pkcs10, application/pkcs7' \
    -H "Authorization: Bearer ${BEARER_TOKEN}" \
    -H 'Content-Transfer-Encoding: application/pkcs10')

  if [ -z "$raw" ]; then
    echo "ERROR: Empty response from $url" >&2
    return 1
  fi

  # Try PKCS7 DER (base64-encoded)
  local certs
  certs=$(echo "$raw" | openssl enc -base64 -d 2>/dev/null | openssl pkcs7 -inform DER -print_certs 2>/dev/null)

  if [ -n "$certs" ]; then
    echo "$certs"
    return 0
  fi

  # Try direct PEM (Hubject may return PEM directly)
  if echo "$raw" | grep -q 'BEGIN CERTIFICATE'; then
    echo "$raw"
    return 0
  fi

  # Try raw base64 decode as PEM
  certs=$(echo "$raw" | openssl enc -base64 -d 2>/dev/null)
  if echo "$certs" | grep -q 'BEGIN CERTIFICATE'; then
    echo "$certs"
    return 0
  fi

  echo "ERROR: Could not decode certificates from $url" >&2
  return 1
}

echo "Fetching CPO CA certificates..."
cpo_certs=$(fetch_certs "https://open.plugncharge-test.hubject.com/.well-known/cpo/cacerts")
if [ $? -ne 0 ] || [ -z "$cpo_certs" ]; then
  echo "WARNING: Failed to fetch CPO certs, E2E tests may fail" >&2
  exit 1
fi

echo "${cpo_certs}" | awk '/subject.*CN.*=.*CPO Sub1 CA/,/END CERTIFICATE/' > "${cert_dir}"/cpo_sub_ca1.pem
echo "${cpo_certs}" | awk '/subject.*CN.*=.*CPO Sub2 CA/,/END CERTIFICATE/' > "${cert_dir}"/cpo_sub_ca2.pem
echo "${cpo_certs}" | awk '/subject.*CN.*=.*V2G Root CA/,/END CERTIFICATE/' > "${cert_dir}"/root-V2G-cert.pem
cat "${cert_dir}"/cpo_sub_ca1.pem "${cert_dir}"/cpo_sub_ca2.pem > "${cert_dir}"/trust.pem

echo "Fetching MO CA certificates..."
mo_certs=$(fetch_certs "https://open.plugncharge-test.hubject.com/.well-known/mo/cacerts")
if [ $? -ne 0 ] || [ -z "$mo_certs" ]; then
  echo "WARNING: Failed to fetch MO certs, E2E tests may fail" >&2
  exit 1
fi

echo "${mo_certs}" | awk '/subject.*CN.*=.*MO Sub1 CA/,/END CERTIFICATE/' > "${cert_dir}"/mo_sub_ca1.pem
echo "${mo_certs}" | awk '/subject.*CN.*=.*MO Sub2 CA/,/END CERTIFICATE/' > "${cert_dir}"/mo_sub_ca2.pem

# Validate that we got actual cert content
for f in cpo_sub_ca1.pem cpo_sub_ca2.pem root-V2G-cert.pem; do
  if [ ! -s "${cert_dir}/$f" ] || ! grep -q 'BEGIN CERTIFICATE' "${cert_dir}/$f" 2>/dev/null; then
    echo "WARNING: ${f} is empty or invalid" >&2
  fi
done

echo "CA certificates retrieved successfully"
