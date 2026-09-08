package clitest_test

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"gotest.tools/v3/assert"
)

const fakeVersion = "2.62.0"

// fakeGHRelease starts a server that stands in for GitHub's release hosting: it
// redirects the "latest" URL to a tag and serves an archive whose bin/gh is a
// script reporting fakeVersion. It returns the base URL to pass as --base-url.
func fakeGHRelease(t *testing.T) string {
	t.Helper()

	ghScript := fmt.Sprintf("#!/bin/sh\ncase \"$1\" in\n  --version) echo 'gh version %s (2024-01-01)' ;;\n  *) exit 0 ;;\nesac\n", fakeVersion)
	archive := buildArchive(t, ghScript)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/cli/cli/releases/latest":
			http.Redirect(w, r, "/cli/cli/releases/tag/v"+fakeVersion, http.StatusFound)
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

	linked, statErr := os.Stat(filepath.Join(s.BinDir, "gh"))
	assert.NilError(t, statErr, "gh not symlinked into bin dir")
	assert.Assert(t, linked != nil)

	bashEnv, readErr := os.ReadFile(s.BashEnv)
	assert.NilError(t, readErr)
	assert.Assert(t, strings.Contains(string(bashEnv), "export PATH="), "BASH_ENV missing PATH: %s", bashEnv)
	assert.Assert(t, strings.Contains(string(bashEnv), filepath.Dir(ghBin)), "BASH_ENV missing gh bin dir: %s", bashEnv)
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
