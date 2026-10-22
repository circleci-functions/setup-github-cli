package ghcli

import (
	"testing"

	"gotest.tools/v3/assert"
)

func TestPlatformFor(t *testing.T) {
	tests := []struct {
		name    string
		goos    string
		goarch  string
		want    Platform
		wantErr bool
	}{
		{name: "linux amd64", goos: "linux", goarch: "amd64", want: Platform{OS: "linux", Arch: "amd64"}},
		{name: "linux arm64", goos: "linux", goarch: "arm64", want: Platform{OS: "linux", Arch: "arm64"}},
		{name: "darwin amd64", goos: "darwin", goarch: "amd64", want: Platform{OS: "macOS", Arch: "amd64"}},
		{name: "darwin arm64", goos: "darwin", goarch: "arm64", want: Platform{OS: "macOS", Arch: "arm64"}},
		{name: "windows unsupported", goos: "windows", goarch: "amd64", wantErr: true},
		{name: "386 unsupported", goos: "linux", goarch: "386", wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := platformFor(tc.goos, tc.goarch)
			if tc.wantErr {
				assert.Assert(t, err != nil)
				return
			}
			assert.NilError(t, err)
			assert.Equal(t, got, tc.want)
		})
	}
}

func TestPlatformAsset(t *testing.T) {
	tests := []struct {
		name     string
		platform Platform
		version  string
		want     string
		wantErr  bool
	}{
		{name: "linux amd64 tar.gz", platform: Platform{"linux", "amd64"}, version: "v2.37.0", want: "gh_2.37.0_linux_amd64.tar.gz"},
		{name: "linux arm64 tar.gz", platform: Platform{"linux", "arm64"}, version: "v2.37.0", want: "gh_2.37.0_linux_arm64.tar.gz"},
		{name: "macOS amd64 pre-2.28 is tar.gz", platform: Platform{"macOS", "amd64"}, version: "v2.27.0", want: "gh_2.27.0_macOS_amd64.tar.gz"},
		{name: "macOS amd64 at 2.28 is zip", platform: Platform{"macOS", "amd64"}, version: "v2.28.0", want: "gh_2.28.0_macOS_amd64.zip"},
		{name: "macOS arm64 zip", platform: Platform{"macOS", "arm64"}, version: "v2.30.0", want: "gh_2.30.0_macOS_arm64.zip"},
		{name: "macOS arm64 at 2.25 boundary", platform: Platform{"macOS", "arm64"}, version: "v2.25.0", want: "gh_2.25.0_macOS_arm64.tar.gz"},
		{name: "macOS arm64 before 2.25 errors", platform: Platform{"macOS", "arm64"}, version: "v2.24.0", wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := tc.platform.Asset(tc.version)
			if tc.wantErr {
				assert.Assert(t, err != nil)
				return
			}
			assert.NilError(t, err)
			assert.Equal(t, got, tc.want)
		})
	}
}

func TestPlatformAssetURL(t *testing.T) {
	got, err := Platform{OS: "linux", Arch: "amd64"}.AssetURL("https://github.com", "v2.37.0")
	assert.NilError(t, err)
	assert.Equal(t, got, "https://github.com/cli/cli/releases/download/v2.37.0/gh_2.37.0_linux_amd64.tar.gz")

	_, err = Platform{OS: "macOS", Arch: "arm64"}.AssetURL("https://github.com", "v2.20.0")
	assert.Assert(t, err != nil)
}

func TestChecksumsURL(t *testing.T) {
	assert.Equal(t, ChecksumsURL("https://github.com/", "v2.62.0"),
		"https://github.com/cli/cli/releases/download/v2.62.0/gh_2.62.0_checksums.txt")
}
