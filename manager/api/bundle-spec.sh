#!/bin/bash
# Bundle the split OpenAPI spec into a single file for code generation

set -e

cd "$(dirname "$0")"

echo "Bundling OpenAPI spec..."

# Use redocly to bundle the spec
npx @redocly/cli bundle api-spec.yaml -o api-spec.bundled.yaml

echo "Successfully created api-spec.bundled.yaml"
