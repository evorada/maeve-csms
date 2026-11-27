// SPDX-License-Identifier: Apache-2.0

package api_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The OpenAPI spec is split: api-spec.yaml (the root) only holds $refs to the path
// items in paths/*.yaml and to the schemas in schemas/schemas.yaml. These tests keep
// it that way, so that an inline copy cannot come back and drift from the split files
// and a new path or schema cannot be forgotten in the root.

const (
	rootSpecFile     = "api-spec.yaml"
	pathFilesPattern = "paths/*.yaml"
	schemasSpecFile  = "schemas/schemas.yaml"
)

// loadSpecFile loads a spec file and resolves its $refs. It fails when a $ref cannot
// be resolved, for example a path file that refers to '#/components/schemas/...'
// instead of '../schemas/schemas.yaml#/components/schemas/...'.
func loadSpecFile(t *testing.T, file string) *openapi3.T {
	t.Helper()

	loader := openapi3.NewLoader()
	loader.IsExternalRefsAllowed = true
	doc, err := loader.LoadFromFile(file)
	require.NoErrorf(t, err, "loading %s", file)
	return doc
}

// jsonPointerToken escapes a key for use in the fragment of a $ref (RFC 6901).
func jsonPointerToken(key string) string {
	return strings.ReplaceAll(strings.ReplaceAll(key, "~", "~0"), "/", "~1")
}

func TestRootSpecPathsAreRefsToPathFiles(t *testing.T) {
	root := loadSpecFile(t, rootSpecFile)

	pathFiles, err := filepath.Glob(pathFilesPattern)
	require.NoError(t, err)
	require.NotEmpty(t, pathFiles, "no path files found")

	// the file that defines each path
	definedIn := make(map[string]string)
	for _, pathFile := range pathFiles {
		pathFile = filepath.ToSlash(pathFile)
		doc := loadSpecFile(t, pathFile)
		require.NotNilf(t, doc.Paths, "%s has no paths", pathFile)
		for path := range doc.Paths.Map() {
			if other, ok := definedIn[path]; ok {
				t.Errorf("path %s is defined in both %s and %s", path, other, pathFile)
			}
			definedIn[path] = pathFile
		}
	}

	require.NotNil(t, root.Paths)
	rootPaths := root.Paths.Map()
	require.NotEmpty(t, rootPaths)

	for path, item := range rootPaths {
		pathFile, ok := definedIn[path]
		if !assert.Truef(t, ok, "path %s in %s is not defined in any %s file", path, rootSpecFile, pathFilesPattern) {
			continue
		}
		assert.Equalf(t, pathFile+"#/paths/"+jsonPointerToken(path), item.Ref,
			"path %s in %s must be a $ref to its definition in %s, not an inline path item", path, rootSpecFile, pathFile)
	}

	for path, pathFile := range definedIn {
		_, ok := rootPaths[path]
		assert.Truef(t, ok, "path %s (%s) is not referenced from %s: add a $ref for it under paths", path, pathFile, rootSpecFile)
	}
}

func TestRootSpecSchemasAreRefsToSchemasFile(t *testing.T) {
	root := loadSpecFile(t, rootSpecFile)
	schemas := loadSpecFile(t, schemasSpecFile)

	require.NotNil(t, root.Components)
	require.NotNil(t, schemas.Components)
	require.NotEmpty(t, root.Components.Schemas)
	require.NotEmpty(t, schemas.Components.Schemas)

	for name, schema := range root.Components.Schemas {
		assert.Equalf(t, schemasSpecFile+"#/components/schemas/"+jsonPointerToken(name), schema.Ref,
			"schema %s in %s must be a $ref to its definition in %s, not an inline schema", name, rootSpecFile, schemasSpecFile)
	}

	for name := range schemas.Components.Schemas {
		_, ok := root.Components.Schemas[name]
		assert.Truef(t, ok, "schema %s (%s) is not referenced from %s: add a $ref for it under components.schemas", name, schemasSpecFile, rootSpecFile)
	}
}
