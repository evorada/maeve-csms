#!/usr/bin/env bash

BEARER_TOKEN=$(curl -s https://hubject.stoplight.io/api/v1/projects/cHJqOjk0NTg5/nodes/6bb8b3bc79c2e-authorization-token | jq -r .data | sed -n '/Bearer/s/^.*Bearer //p')

script_dir=$( cd -- "$( dirname -- "${BASH_SOURCE[0]}" )" &> /dev/null && pwd )

certs=$(curl --request GET \
  --url https://open.plugncharge-test.hubject.com/.well-known/cpo/cacerts \
  --header 'Accept: application/pkcs10, application/json' \
  --header "Authorization: Bearer ${BEARER_TOKEN}" | openssl enc -base64 -d | openssl pkcs7 -inform DER -print_certs)

echo "${certs}" | awk '/subject.*CN.*=.*CPO Sub1 CA/,/END CERTIFICATE/' > "${script_dir}"/../config/certificates/cpo_sub_ca1.pem
echo "${certs}" | awk '/subject.*CN.*=.*CPO Sub2 CA/,/END CERTIFICATE/' > "${script_dir}"/../config/certificates/cpo_sub_ca2.pem
echo "${certs}" | awk '/subject.*CN.*=.*V2G Root CA/,/END CERTIFICATE/' > "${script_dir}"/../config/certificates/root-V2G-cert.pem
cat "${script_dir}"/../config/certificates/cpo_sub_ca1.pem "${script_dir}"/../config/certificates/cpo_sub_ca2.pem > "${script_dir}"/../config/certificates/trust.pem
