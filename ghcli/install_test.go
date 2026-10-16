package ghcli

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"gotest.tools/v3/assert"
	"gotest.tools/v3/skip"
)

func TestResolveVersionExplicit(t *testing.T) {
	tests := []struct {
		name    string
		spec    string
		want    string
		wantErr bool
	}{
		{name: "bare semver gets v prefix", spec: "2.37.0", want: "v2.37.0"},
		{name: "already prefixed", spec: "v2.37.0", want: "v2.37.0"},
		{name: "major.minor", spec: "2.37", want: "v2.37"},
		{name: "invalid", spec: "not-a-version", wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ResolveVersion(context.Background(), "", tc.spec, 0)
			if tc.wantErr {
				assert.Assert(t, err != nil)
				return
			}
			assert.NilError(t, err)
			assert.Equal(t, got, tc.want)
		})
	}
}

func TestResolveVersionLatest(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/cli/cli/releases/latest" {
			http.Redirect(w, r, "/cli/cli/releases/tag/v2.62.0", http.StatusFound)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	got, err := ResolveVersion(context.Background(), srv.URL, "latest", 2)
	assert.NilError(t, err)
	assert.Equal(t, got, "v2.62.0")

	empty, err := ResolveVersion(context.Background(), srv.URL, "", 2)
	assert.NilError(t, err)
	assert.Equal(t, empty, "v2.62.0")
}

func TestResolveVersionLatestRetryExhausted(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	_, err := ResolveVersion(context.Background(), srv.URL, "latest", 1)
	assert.Assert(t, err != nil)
}

func TestStripFirstComponent(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "wrapper dir itself", in: "gh_2.37.0_linux_amd64/", want: ""},
		{name: "nested file", in: "gh_2.37.0_linux_amd64/bin/gh", want: "bin/gh"},
		{name: "leading dot slash", in: "./gh_2.37.0_linux_amd64/bin/gh", want: "bin/gh"},
		{name: "no slash", in: "gh_2.37.0_linux_amd64", want: ""},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, stripFirstComponent(tc.in), tc.want)
		})
	}
}

func TestExtractTarball(t *testing.T) {
	var buf bytes.Buffer
	gzw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gzw)
	writeTar := func(name string, mode int64, body string) {
		assert.NilError(t, tw.WriteHeader(&tar.Header{Name: name, Mode: mode, Size: int64(len(body)), Typeflag: tar.TypeReg}))
		_, err := tw.Write([]byte(body))
		assert.NilError(t, err)
	}
	writeTar("gh_2.37.0_linux_amd64/bin/gh", 0755, "#!/bin/sh\n")
	writeTar("gh_2.37.0_linux_amd64/share/man/man1/gh.1", 0644, "manpage")
	assert.NilError(t, tw.Close())
	assert.NilError(t, gzw.Close())

	src := filepath.Join(t.TempDir(), "gh.tar.gz")
	assert.NilError(t, os.WriteFile(src, buf.Bytes(), 0600))

	dest := t.TempDir()
	assert.NilError(t, extractTarball(src, dest))

	gh, err := os.ReadFile(filepath.Join(dest, "bin", "gh"))
	assert.NilError(t, err)
	assert.Equal(t, string(gh), "#!/bin/sh\n")
	man, err := os.ReadFile(filepath.Join(dest, "share", "man", "man1", "gh.1"))
	assert.NilError(t, err)
	assert.Equal(t, string(man), "manpage")
}

func TestExtractZip(t *testing.T) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	writeZip := func(name, body string) {
		w, err := zw.Create(name)
		assert.NilError(t, err)
		_, err = w.Write([]byte(body))
		assert.NilError(t, err)
	}
	writeZip("gh_2.37.0_macOS_arm64/bin/gh", "#!/bin/sh\n")
	writeZip("gh_2.37.0_macOS_arm64/share/man/man1/gh.1", "manpage")
	assert.NilError(t, zw.Close())

	src := filepath.Join(t.TempDir(), "gh.zip")
	assert.NilError(t, os.WriteFile(src, buf.Bytes(), 0600))

	dest := t.TempDir()
	assert.NilError(t, extractZip(src, dest))

	gh, err := os.ReadFile(filepath.Join(dest, "bin", "gh"))
	assert.NilError(t, err)
	assert.Equal(t, string(gh), "#!/bin/sh\n")
}

func TestInstalledVersion(t *testing.T) {
	skip.If(t, runtime.GOOS == "windows", "fake gh uses a shell script")

	dir := t.TempDir()
	ghBin := filepath.Join(dir, "gh")
	assert.NilError(t, os.WriteFile(ghBin, []byte("#!/bin/sh\necho 'gh version 2.37.0 (2023-01-01)'\n"), 0700))

	assert.Equal(t, installedVersion(ghBin), "v2.37.0")
	assert.Equal(t, installedVersion(filepath.Join(dir, "absent")), "")

	notGH := filepath.Join(dir, "git")
	assert.NilError(t, os.WriteFile(notGH, []byte("#!/bin/sh\necho 'git version 2.40.0'\n"), 0700))
	assert.Equal(t, installedVersion(notGH), "")
}

func TestVerifyGH(t *testing.T) {
	skip.If(t, runtime.GOOS == "windows", "fake gh uses a shell script")

	fakeGH := func(t *testing.T, output string) string {
		t.Helper()
		dir := t.TempDir()
		assert.NilError(t, os.WriteFile(filepath.Join(dir, "gh"), []byte("#!/bin/sh\necho '"+output+"'\n"), 0700))
		return dir
	}
	ours := fakeGH(t, "gh version 2.62.0 (2024-01-01)")
	other := fakeGH(t, "gh version 2.62.0 (2024-01-01)")
	older := fakeGH(t, "gh version 2.37.0 (2023-01-01)")
	notGH := fakeGH(t, "git version 2.40.0")

	link := filepath.Join(t.TempDir(), "gh")
	assert.NilError(t, os.Symlink(filepath.Join(ours, "gh"), link))

	tests := []struct {
		name    string
		from    string
		bin     string
		wantErr string
	}{
		{name: "our gh through a link", from: ours, bin: link},
		{name: "older gh", from: ours, bin: filepath.Join(older, "gh"), wantErr: "reports gh v2.37.0 rather than v2.62.0"},
		{name: "same version elsewhere", from: ours, bin: filepath.Join(other, "gh"), wantErr: "is not " + filepath.Join(ours, "gh")},
		{name: "lookalike", from: ours, bin: filepath.Join(notGH, "gh"), wantErr: "not a gh version"},
		{name: "absent", from: ours, bin: filepath.Join(t.TempDir(), "gh"), wantErr: "could not report"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := verifyGH(tc.from, "v2.62.0")(tc.bin)
			if tc.wantErr == "" {
				assert.NilError(t, err)
				return
			}
			assert.ErrorContains(t, err, tc.wantErr)
		})
	}
}
