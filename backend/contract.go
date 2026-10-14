package contract

import _ "embed"

// shortcut: module-root embed because go:embed cannot see a parent directory.
//
//go:embed openapi.yaml
var OpenAPI []byte
