// SPDX-License-Identifier: Apache-2.0

package schemas

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"net/url"

	"github.com/santhosh-tekuri/jsonschema"
	"github.com/santhosh-tekuri/jsonschema/loader"
)

type FSLoader struct {
	FS fs.FS
}

func (f FSLoader) Load(u string) (io.ReadCloser, error) {
	parsedUrl, err := url.Parse(u)
	if err != nil {
		return nil, err
	}
	if parsedUrl.Scheme == "fs" {
		return f.FS.Open(parsedUrl.Path[1:])
	}
	return nil, nil
}

// Compile loads the schema file from the file system and compiles it. It fails
// when the file is missing or is not a valid JSON schema.
func Compile(schemaFs fs.FS, schemaFile string) (*jsonschema.Schema, error) {
	compiler := jsonschema.NewCompiler()
	compiler.Draft = jsonschema.Draft6
	loader.Register("fs", FSLoader{
		FS: schemaFs,
	})
	return compiler.Compile(fmt.Sprintf("fs:///%s", schemaFile))
}

// Validate checks the data against the schema file. A *jsonschema.ValidationError is
// returned when the data does not match the schema; any other error means that the
// schema could not be loaded or compiled.
func Validate(data json.RawMessage, schemaFs fs.FS, schemaFile string) error {
	schema, err := Compile(schemaFs, schemaFile)
	if err != nil {
		return err
	}

	return schema.Validate(bytes.NewReader(data))
}
