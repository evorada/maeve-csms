#!/bin/bash
# Bundle the split OpenAPI spec into a single file for code generation.
#
# Run this script after modifying any file in:
#   - manager/api/api-spec.yaml (root spec)
#   - manager/api/paths/*.yaml
#   - manager/api/schemas/*.yaml
#
# The bundled output (api-spec.bundled.yaml) is committed to the repo
# so that oapi-codegen and docs generation work without needing redocly
# installed. CI will fail if the bundled file is out of date.
#
# Usage:
#   ./bundle-spec.sh          # from manager/api/
#   make api-bundle            # from manager/
#   make api-generate          # bundle + regenerate api.gen.go

set -e

cd "$(dirname "$0")"

echo "Bundling OpenAPI spec..."

# Use redocly to bundle the spec
npx @redocly/cli bundle api-spec.yaml -o api-spec.bundled.yaml

echo "Successfully created api-spec.bundled.yaml"
