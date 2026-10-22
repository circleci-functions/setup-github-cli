package clitest_test

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"

	"gotest.tools/v3/assert"
)

const fakeVersion = "2.62.0"

// fakeGHRelease starts a server that stands in for GitHub's release hosting: it
// redirects the "latest" URL to a tag and serves an archive whose bin/gh is a
// script reporting fakeVersion, plus a checksums file listing its SHA-256. It
// returns the base URL to pass as --base-url.
func fakeGHRelease(t *testing.T) string {
	t.Helper()
	return fakeGHReleaseWithChecksums(t, nil)
}

// fakeGHReleaseWithChecksums is fakeGHRelease with the checksums file that
// checksums builds from the archive, or one listing the archive's real SHA-256
// when checksums is nil.
func fakeGHReleaseWithChecksums(t *testing.T, checksums func(archive []byte) string) string {
	t.Helper()

	ghScript := fmt.Sprintf("#!/bin/sh\ncase \"$1\" in\n  --version) echo 'gh version %s (2024-01-01)' ;;\n  *) exit 0 ;;\nesac\n", fakeVersion)
	archive := buildArchive(t, ghScript)
	if checksums == nil {
		checksums = func(archive []byte) string {
			return fmt.Sprintf("%x  %s\n", sha256.Sum256(archive), hostAsset())
		}
	}
	sums := checksums(archive)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/cli/cli/releases/latest":
			http.Redirect(w, r, "/cli/cli/releases/tag/v"+fakeVersion, http.StatusFound)
		case r.URL.Path == "/cli/cli/releases/download/v"+fakeVersion+"/gh_"+fakeVersion+"_checksums.txt":
			_, _ = w.Write([]byte(sums))
		case strings.HasPrefix(r.URL.Path, "/cli/cli/releases/download/"):
			_, _ = w.Write(archive)
		default:
			w.WriteHeader(http.StatusOK)
		}
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}

// buildArchive returns a gh release archive for the host platform: a .zip on
// macOS, a .tar.gz elsewhere, matching what setup-github-cli downloads.
func buildArchive(t *testing.T, ghScript string) []byte {
	t.Helper()
	if runtime.GOOS == "darwin" {
		var buf bytes.Buffer
		zw := zip.NewWriter(&buf)
		hdr := &zip.FileHeader{Name: "gh_" + fakeVersion + "/bin/gh"}
		hdr.SetMode(0755)
		w, err := zw.CreateHeader(hdr)
		assert.NilError(t, err)
		_, err = w.Write([]byte(ghScript))
		assert.NilError(t, err)
		assert.NilError(t, zw.Close())
		return buf.Bytes()
	}

	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	assert.NilError(t, tw.WriteHeader(&tar.Header{
		Name: "gh_" + fakeVersion + "/bin/gh", Mode: 0755, Size: int64(len(ghScript)), Typeflag: tar.TypeReg,
	}))
	_, err := tw.Write([]byte(ghScript))
	assert.NilError(t, err)
	assert.NilError(t, tw.Close())
	assert.NilError(t, gz.Close())
	return buf.Bytes()
}

// hostAsset is the name of the release asset setup-github-cli downloads on this
// platform.
func hostAsset() string {
	if runtime.GOOS == "darwin" {
		return fmt.Sprintf("gh_%s_macOS_%s.zip", fakeVersion, runtime.GOARCH)
	}
	return fmt.Sprintf("gh_%s_linux_%s.tar.gz", fakeVersion, runtime.GOARCH)
}

// TestCLI_RejectsBadChecksum serves an archive the checksums file does not
// vouch for. The step must fail without extracting or linking anything.
func TestCLI_RejectsBadChecksum(t *testing.T) {
	tests := []struct {
		name      string
		checksums func(archive []byte) string
		wantErr   string
	}{
		{
			name: "mismatch",
			checksums: func([]byte) string {
				return fmt.Sprintf("%x  %s\n", sha256.Sum256([]byte("something else")), hostAsset())
			},
			wantErr: "checksum mismatch for " + hostAsset(),
		},
		{
			name: "asset not listed",
			checksums: func(archive []byte) string {
				return fmt.Sprintf("%x  gh_%s_windows_amd64.zip\n", sha256.Sum256(archive), fakeVersion)
			},
			wantErr: "lists no checksum for " + hostAsset(),
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s, run := sandbox(t)
			base := fakeGHReleaseWithChecksums(t, tc.checksums)

			out, err := run(t, nil,
				"--version", fakeVersion,
				"--install-dir", s.InstallDir,
				"--bin-dir", s.BinDir,
				"--base-url", base,
			)
			assert.Assert(t, err != nil, "expected non-zero exit; got: %s", out)
			assert.Assert(t, strings.Contains(out, tc.wantErr), "got: %s", out)

			_, statErr := os.Stat(filepath.Join(s.InstallDir, "v"+fakeVersion))
			assert.Assert(t, os.IsNotExist(statErr), "extracted despite a bad checksum; got: %s", out)
			_, statErr = os.Lstat(filepath.Join(s.BinDir, "gh"))
			assert.Assert(t, os.IsNotExist(statErr), "linked despite a bad checksum; got: %s", out)
		})
	}
}

func TestCLI_Help(t *testing.T) {
	_, run := sandbox(t)
	out, err := run(t, nil, "--help")
	assert.NilError(t, err, "got: %s", out)
	assert.Assert(t, strings.Contains(out, "Install the GitHub CLI"), "got: %s", out)
	assert.Assert(t, strings.Contains(out, "--version"), "got: %s", out)
}

func TestCLI_Describe(t *testing.T) {
	_, run := sandbox(t)
	out, err := run(t, nil, "--describe")
	assert.NilError(t, err, "got: %s", out)

	assert.Assert(t, strings.Contains(out, "name: setup-github-cli"), "got: %s", out)
	for _, flag := range []string{"version", "max-retries", "install-dir", "configure", "token", "hostname"} {
		assert.Assert(t, strings.Contains(out, "name: "+flag), "descriptor missing flag %q: %s", flag, out)
	}
	for _, hidden := range []string{"bin-dir", "base-url"} {
		assert.Assert(t, !strings.Contains(out, "name: "+hidden), "descriptor exposes hidden flag %q: %s", hidden, out)
	}
}

func TestCLI_UnknownFlag(t *testing.T) {
	_, run := sandbox(t)
	_, err := run(t, nil, "--no-such-flag")
	assert.Assert(t, err != nil, "expected non-zero exit for unknown flag")
}

func TestCLI_InvalidVersion(t *testing.T) {
	_, run := sandbox(t)
	out, err := run(t, nil, "--version", "not-a-version")
	assert.Assert(t, err != nil, "expected non-zero exit for invalid version; got: %s", out)
}

func TestCLI_InstallsGh(t *testing.T) {
	s, run := sandbox(t)
	base := fakeGHRelease(t)

	out, err := run(t, nil,
		"--version", fakeVersion,
		"--install-dir", s.InstallDir,
		"--bin-dir", s.BinDir,
		"--base-url", base,
	)
	assert.NilError(t, err, "got: %s", out)

	ghBin := filepath.Join(s.InstallDir, "v"+fakeVersion, "bin", "gh")
	_, statErr := os.Stat(ghBin)
	assert.NilError(t, statErr, "gh not installed at %s; got: %s", ghBin, out)

	// gh is put on PATH by linking it into the requested bin dir, not by
	// exporting PATH to $BASH_ENV.
	target, readErr := os.Readlink(filepath.Join(s.BinDir, "gh"))
	assert.NilError(t, readErr, "gh not linked into bin dir; got: %s", out)
	assert.Assert(t, strings.HasPrefix(target, s.InstallDir),
		"expected the link to point inside %s, got %q", s.InstallDir, target)

	_, statErr = os.Stat(s.BashEnv)
	assert.Assert(t, os.IsNotExist(statErr), "install must not write $BASH_ENV; it links gh onto PATH")
}

func TestCLI_ConfigureExportsToken(t *testing.T) {
	s, run := sandbox(t)
	base := fakeGHRelease(t)

	out, err := run(t, map[string]string{"MY_TOKEN": "tok-value"},
		"--version", fakeVersion,
		"--install-dir", s.InstallDir,
		"--bin-dir", s.BinDir,
		"--base-url", base,
		"--configure",
		"--token", "MY_TOKEN",
	)
	assert.NilError(t, err, "got: %s", out)

	bashEnv, readErr := os.ReadFile(s.BashEnv)
	assert.NilError(t, readErr)
	assert.Assert(t, strings.Contains(string(bashEnv), "GITHUB_TOKEN"), "BASH_ENV missing GITHUB_TOKEN: %s", bashEnv)
	assert.Assert(t, strings.Contains(string(bashEnv), "tok-value"), "BASH_ENV missing token value: %s", bashEnv)
}

// TestCLI_ConfigureWithoutToken passes a token value where --token wants a
// variable name, a likely mistake. The step must fail before downloading
// anything, without printing the value back.
func TestCLI_ConfigureWithoutToken(t *testing.T) {
	s, run := sandbox(t)
	var requests atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(srv.Close)

	const pasted = "ghp_REALSECRET123"
	out, err := run(t, nil,
		"--version", fakeVersion,
		"--install-dir", s.InstallDir,
		"--bin-dir", s.BinDir,
		"--base-url", srv.URL,
		"--configure",
		"--token", pasted,
	)
	assert.Assert(t, err != nil, "expected non-zero exit; got: %s", out)
	assert.Assert(t, strings.Contains(out, "not the token itself"), "got: %s", out)
	assert.Assert(t, !strings.Contains(out, pasted), "output echoes --token: %s", out)
	assert.Equal(t, requests.Load(), int32(0), "downloaded before checking the token")
}
