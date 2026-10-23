package handler

import (
	"net/http"

	toolboxapi "github.com/susanta96/toolbox-backend/api"
)

const scalarHTML = `<!doctype html>
<html>
<head>
  <title>Toolbox API Docs</title>
  <meta charset="utf-8" />
  <meta name="viewport" content="width=device-width, initial-scale=1" />
</head>
<body>
  <script
    id="api-reference"
    data-url="/openapi.yaml"
    data-configuration='{"theme":"purple"}'
  ></script>
  <script src="https://cdn.jsdelivr.net/npm/@scalar/api-reference"></script>
</body>
</html>`

// ServeOpenAPISpec serves the raw OpenAPI YAML spec.
func ServeOpenAPISpec(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/yaml")
	w.Write(toolboxapi.Spec) //nolint:errcheck
}

// ServeScalarDocs serves the Scalar API reference UI.
func ServeScalarDocs(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write([]byte(scalarHTML)) //nolint:errcheck
}
