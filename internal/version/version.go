// Package version carries the build identity stamped in at release time.
//
// Release builds override the defaults with -ldflags, e.g.
//
//	go build -ldflags "-X file-cleaner/internal/version.Version=0.1.0 \
//	                   -X file-cleaner/internal/version.Commit=abc1234" ./cmd/server
package version

// Build identity. The defaults are what a plain `go build` reports.
var (
	// Version is the release version, semver without the leading "v".
	Version = "dev"
	// Commit is the short git commit the binary was built from.
	Commit = "none"
)

// String renders the build identity for logs and /api/v1/system/info.
func String() string {
	if Commit == "" || Commit == "none" {
		return Version
	}
	return Version + " (" + Commit + ")"
}
