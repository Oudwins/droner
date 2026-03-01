package version

import "strings"

// SemVer is set at build time for releases.
//
// Example:
//
//	-ldflags "-X github.com/Oudwins/droner/pkgs/droner/internals/version.SemVer=1.2.3"
var SemVer = "0.0.0-dev"

// BuiltAt is set at build time for releases.
//
// Example:
//
//	-ldflags "-X github.com/Oudwins/droner/pkgs/droner/internals/version.BuiltAt=2026-02-28T00:00:00Z"
var BuiltAt = ""

// Version returns a SemVer string with best-effort build metadata identity.
//
// Examples:
//   - 1.2.3+a1b2c3d4e5f6.9f2c1a0b77de
//   - 0.0.0-dev+a1b2c3d4e5f6.dirty.1e4b9caa2210
func Version() string {
	v := strings.TrimSpace(SemVer)
	if v == "" {
		v = "0.0.0-dev"
	}
	meta := strings.TrimSpace(IdentityMetadata())
	if meta == "" {
		return v
	}
	if strings.Contains(v, "+") {
		return v + "." + meta
	}
	return v + "+" + meta
}
