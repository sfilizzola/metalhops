package contract

import _ "embed"

// shortcut: module-root embed because go:embed cannot see a parent directory; move with the HTTP routes in Session 3.
//
//go:embed openapi.yaml
var OpenAPI []byte
