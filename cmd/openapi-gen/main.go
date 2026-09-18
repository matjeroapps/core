// Command openapi-gen regenerates the committed OpenAPI documents from the Go
// route declarations that own them.
//
// The generated files are never hand-edited. CI runs this command and fails when
// the result differs from what is committed, so a route change that is not
// reflected in the spec cannot merge.
package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/getkin/kin-openapi/openapi3"

	"core/internal/coreapi"
	"core/modules/openapi"
)

func main() {
	specs := []struct {
		path  string
		build func() (*openapi3.T, error)
	}{
		{path: "docs/api/internal/openapi.json", build: coreapi.BuildInternalSpec},
	}

	for _, spec := range specs {
		doc, err := spec.build()
		if err != nil {
			fail(err)
		}
		data, err := openapi.MarshalDocument(doc)
		if err != nil {
			fail(err)
		}
		if err := os.MkdirAll(filepath.Dir(spec.path), 0o755); err != nil {
			fail(err)
		}
		if err := os.WriteFile(spec.path, data, 0o644); err != nil {
			fail(err)
		}
	}
}

func fail(err error) {
	_, _ = fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
