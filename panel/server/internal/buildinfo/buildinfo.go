// Package buildinfo carries the panel version stamped in at build time.
package buildinfo

// Version is set by the image build:
//
//	go build -ldflags "-X github.com/azabash/hapanel/panel/internal/buildinfo.Version=0.1.76" ./cmd/panel
//
// A build without the flag reports "dev".
var Version = "dev"
