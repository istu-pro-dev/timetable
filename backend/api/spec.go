// Package apispec embeds the hand-written OpenAPI 3.1 specification of the HTTP API.
package apispec

import _ "embed"

// OpenAPI is the contents of openapi.yaml, served at GET /api/openapi.yaml.
//
//go:embed openapi.yaml
var OpenAPI []byte
