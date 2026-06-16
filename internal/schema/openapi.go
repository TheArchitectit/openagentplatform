package schema

import (
	_ "embed"
	"net/http"
	"sync"

	"github.com/go-chi/chi/v5"
	"sigs.k8s.io/yaml"
)

//go:embed openapi.yaml
var openapiYAML []byte

var (
	jsonOnce   sync.Once
	jsonSpec   []byte
	jsonErr    error
)

func toJSON() ([]byte, error) {
	jsonOnce.Do(func() {
		jsonSpec, jsonErr = yaml.YAMLToJSON(openapiYAML)
	})
	return jsonSpec, jsonErr
}

// MountSwagger wires the API docs endpoints:
//   - GET /docs           — Swagger UI HTML (CDN-hosted assets)
//   - GET /docs/swagger   — raw OpenAPI YAML
//   - GET /docs/openapi.json — OpenAPI spec as JSON (yaml → json)
func MountSwagger(r chi.Router) {
	r.Get("/docs", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(swaggerHTML))
	})

	r.Get("/docs/swagger", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/yaml; charset=utf-8")
		_, _ = w.Write(openapiYAML)
	})

	r.Get("/docs/openapi.json", func(w http.ResponseWriter, _ *http.Request) {
		data, err := toJSON()
		if err != nil {
			http.Error(w, `{"error":"spec_conversion_failed"}`, http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_, _ = w.Write(data)
	})
}

const swaggerHTML = `<!doctype html>
<html lang="en"><head>
<meta charset="utf-8">
<title>OAP API Docs</title>
<meta name="viewport" content="width=device-width, initial-scale=1">
<link rel="stylesheet" href="https://unpkg.com/swagger-ui-dist@5/swagger-ui.css">
<style>body{margin:0}#swagger{max-width:1400px;margin:0 auto;padding:16px}</style>
</head><body><div id="swagger"></div>
<script src="https://unpkg.com/swagger-ui-dist@5/swagger-ui-bundle.js"></script>
<script>
window.onload = function() {
  SwaggerUIBundle({
    url: "/docs/openapi.json",
    dom_id: "#swagger",
    deepLinking: true,
    presets: [SwaggerUIBundle.presets.apis]
  });
};
</script>
</body></html>`
