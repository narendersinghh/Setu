// Package buildinfo holds the version stamped at build time:
// go build -ldflags "-X github.com/narendersinghh/setu/server/internal/buildinfo.Version=2.0.0 -X …Commit=abc123"
package buildinfo

var (
	Version = "dev"
	Commit  = ""
)
