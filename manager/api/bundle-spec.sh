#!/bin/bash
# Bundle the split OpenAPI spec into a single file for code generation.
#
# The root spec (api-spec.yaml) only holds $refs to the files in:
#   - manager/api/paths/*.yaml
#   - manager/api/schemas/schemas.yaml
#
# Run this script (or better: make api-generate) after modifying any of them.
#
# The bundled output (api-spec.bundled.yaml) is committed to the repo
# so that oapi-codegen and docs generation work without needing redocly
# installed. CI will fail if the bundled file or api.gen.go is out of date.
#
# The redocly version is locked by package-lock.json. A $ref that cannot be
# resolved makes redocly, and so this script, fail.
#
# Usage:
#   ./bundle-spec.sh          # from manager/api/
#   make api-bundle            # from manager/
#   make api-generate          # bundle + regenerate api.gen.go

set -e

cd "$(dirname "$0")"

if [ ! -d node_modules/@redocly/cli ]; then
  echo "Installing the locked @redocly/cli version..."
  npm ci --no-audit --no-fund
fi

echo "Bundling OpenAPI spec..."

# Use the locked redocly version to bundle the spec
npx --no-install @redocly/cli bundle api-spec.yaml -o api-spec.bundled.yaml

echo "Successfully created api-spec.bundled.yaml"
