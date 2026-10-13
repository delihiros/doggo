package doggo

import "embed"

// ToolchainSource contains the Go host needed to build a project-local runtime.
//
//go:embed source_bundle.go cmd internal stdlib go.mod go.sum LICENSE THIRD_PARTY_NOTICES
var ToolchainSource embed.FS
