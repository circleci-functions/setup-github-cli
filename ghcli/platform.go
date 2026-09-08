// Package ghcli installs the GitHub CLI (gh) from the official releases and,
// optionally, authenticates it with a Personal Access Token.
package ghcli

import (
	"fmt"
	"runtime"
	"strings"

	"golang.org/x/mod/semver"
)

// Platform is a GitHub CLI release target, using the OS and architecture names
// that appear in gh release asset filenames (e.g. "macOS", "linux", "amd64").
type Platform struct {
	OS   string
	Arch string
}

// zipMinVersion is the first gh release whose macOS assets are .zip rather than
// .tar.gz (https://github.com/cli/cli/releases/tag/v2.28.0).
const zipMinVersion = "v2.28.0"

// macARM64MinVersion is the first gh release with a macOS arm64 asset
// (https://github.com/cli/cli/releases/tag/v2.25.0).
const macARM64MinVersion = "v2.25.0"

// DetectPlatform returns the release target for the current OS and
// architecture, or an error on a platform gh publishes no asset for.
func DetectPlatform() (Platform, error) {
	return platformFor(runtime.GOOS, runtime.GOARCH)
}

func platformFor(goos, goarch string) (Platform, error) {
	var os string
	switch goos {
	case "linux":
		os = "linux"
	case "darwin":
		os = "macOS"
	default:
		return Platform{}, fmt.Errorf("unsupported operating system %q", goos)
	}

	switch goarch {
	case "amd64", "arm64":
		return Platform{OS: os, Arch: goarch}, nil
	default:
		return Platform{}, fmt.Errorf("unsupported architecture %q", goarch)
	}
}

// extension returns the archive extension gh uses for this platform at version
// (a semver tag like "v2.37.0"): macOS switched from tar.gz to zip at 2.28.0;
// linux is always tar.gz.
func (p Platform) extension(version string) string {
	if p.OS == "macOS" && semver.Compare(version, zipMinVersion) >= 0 {
		return "zip"
	}
	return "tar.gz"
}

// Asset returns the release asset filename for version (a semver tag like
// "v2.37.0"). It errors on a target gh has no asset for at that version, such
// as macOS arm64 before 2.25.0.
func (p Platform) Asset(version string) (string, error) {
	if p.OS == "macOS" && p.Arch == "arm64" && semver.Compare(version, macARM64MinVersion) < 0 {
		return "", fmt.Errorf("macOS arm64 is only available from gh %s onwards, got %s", macARM64MinVersion, version)
	}
	num := strings.TrimPrefix(version, "v")
	return fmt.Sprintf("gh_%s_%s_%s.%s", num, p.OS, p.Arch, p.extension(version)), nil
}

// AssetURL returns the download URL for version's asset on this platform.
func (p Platform) AssetURL(version string) (string, error) {
	asset, err := p.Asset(version)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("https://github.com/cli/cli/releases/download/%s/%s", version, asset), nil
}
