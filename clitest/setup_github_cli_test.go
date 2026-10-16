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
	"os/exec"
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
	for _, spec := range []string{"not-a-version", "2.37"} {
		t.Run(spec, func(t *testing.T) {
			_, run := sandbox(t)
			out, err := run(t, nil, "--version", spec)
			assert.Assert(t, err != nil, "expected non-zero exit for invalid version; got: %s", out)
			assert.Assert(t, strings.Contains(out, "use a full X.Y.Z release"), "got: %s", out)
		})
	}
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

// TestCLI_LinksAheadOfOlderGh puts a gh of another version on PATH first. It
// must not count as installed: later steps have to resolve the requested one.
func TestCLI_LinksAheadOfOlderGh(t *testing.T) {
	tests := []struct {
		name string
		// userBin puts ~/.local/bin on PATH ahead of the older gh.
		userBin bool
	}{
		{name: "links into a user bin dir ahead of it", userBin: true},
		{name: "takes over its directory", userBin: false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s, run := sandbox(t)
			base := fakeGHRelease(t)

			oldDir := t.TempDir()
			oldGH := filepath.Join(oldDir, "gh")
			assert.NilError(t, os.WriteFile(oldGH, []byte("#!/bin/sh\necho 'gh version 2.37.0 (2023-01-01)'\n"), 0700))

			path := []string{oldDir, "/usr/bin", "/bin"}
			if tc.userBin {
				userBin := filepath.Join(s.Home, ".local", "bin")
				assert.NilError(t, os.MkdirAll(userBin, 0750))
				path = append([]string{userBin}, path...)
			}
			pathEnv := strings.Join(path, string(os.PathListSeparator))

			out, err := run(t, map[string]string{"PATH": pathEnv},
				"--version", fakeVersion,
				"--install-dir", s.InstallDir,
				"--base-url", base,
			)
			assert.NilError(t, err, "got: %s", out)

			cmd := exec.CommandContext(t.Context(), "sh", "-c", "gh --version")
			cmd.Env = append(os.Environ(), "PATH="+pathEnv)
			ghOut, err := cmd.CombinedOutput()
			assert.NilError(t, err, "got: %s", ghOut)
			assert.Assert(t, strings.Contains(string(ghOut), "gh version "+fakeVersion), "later steps resolve: %s", ghOut)
		})
	}
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
