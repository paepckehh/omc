// Package version holds the hardwired build version of omc.
//
// The Version string is the always-current semver tag of the program. It is
// the source of truth reported at startup and is meant to be kept in sync
// with the latest git tag.
//
// It can be overridden at link time with the -ldflags "-X" linker flag, for
// example:
//
//	go build -ldflags "-X paepcke.de/omc/internal/version.Version=v0.1.13" .
//
// The Makefile already wires this override via `VERSION ?= $(shell git
// describe --tags --abbrev=0 ...)`, so a release build stamps the binary
// with the exact git tag it was built from. When no override is supplied
// the hardwired constant below is used.
package version

// Version is the program version. Keep this in sync with the latest git
// tag (see AGENTS.md "Release / version stamping"). It may be overridden
// at link time via the linker -X flag.
var Version = "v0.1.19"
