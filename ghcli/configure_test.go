package ghcli

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"gotest.tools/v3/assert"
	"gotest.tools/v3/skip"
)

func TestTokenExports(t *testing.T) {
	tests := []struct {
		name     string
		hostname string
		want     map[string]string
	}{
		{
			name:     "github.com uses GITHUB_TOKEN",
			hostname: "github.com",
			want:     map[string]string{"GITHUB_TOKEN": "tok"},
		},
		{
			name:     "enterprise uses GH_ENTERPRISE_TOKEN and GH_HOST",
			hostname: "ghe.example.com",
			want:     map[string]string{"GH_ENTERPRISE_TOKEN": "tok", "GH_HOST": "ghe.example.com"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.DeepEqual(t, tokenExports(tc.hostname, "tok"), tc.want)
		})
	}
}

func TestConfigureEmptyToken(t *testing.T) {
	t.Setenv("MY_TOKEN", "")
	err := Configure(context.Background(), "gh", ConfigureOptions{Hostname: "github.com", TokenEnvVar: "MY_TOKEN"})
	assert.ErrorContains(t, err, "MY_TOKEN")
}

func TestConfigureExportsToken(t *testing.T) {
	skip.If(t, runtime.GOOS == "windows", "fake gh uses a shell script")

	dir := t.TempDir()
	ghBin := filepath.Join(dir, "gh")
	assert.NilError(t, os.WriteFile(ghBin, []byte("#!/bin/sh\nexit 0\n"), 0700))

	bashEnv := filepath.Join(dir, "bash_env")
	t.Setenv("BASH_ENV", bashEnv)
	t.Setenv("MY_TOKEN", "secret-token")

	err := Configure(context.Background(), ghBin, ConfigureOptions{Hostname: "github.com", TokenEnvVar: "MY_TOKEN"})
	assert.NilError(t, err)

	data, err := os.ReadFile(bashEnv)
	assert.NilError(t, err)
	assert.Assert(t, strings.Contains(string(data), "GITHUB_TOKEN"))
	assert.Assert(t, strings.Contains(string(data), "secret-token"))
}

func TestConfigureTokenQuoting(t *testing.T) {
	skip.If(t, runtime.GOOS == "windows", "fake gh uses a shell script")

	dir := t.TempDir()
	ghBin := filepath.Join(dir, "gh")
	assert.NilError(t, os.WriteFile(ghBin, []byte("#!/bin/sh\nexit 0\n"), 0700))

	bashEnv := filepath.Join(dir, "bash_env")
	t.Setenv("BASH_ENV", bashEnv)
	t.Setenv("MY_TOKEN", "secret$(dangerous)token")

	err := Configure(context.Background(), ghBin, ConfigureOptions{Hostname: "github.com", TokenEnvVar: "MY_TOKEN"})
	assert.NilError(t, err)

	data, err := os.ReadFile(bashEnv)
	assert.NilError(t, err)
	assert.Assert(t, strings.Contains(string(data), "'secret$(dangerous)token'"), "token must be single-quoted; got: %s", string(data))

	fi, err := os.Stat(bashEnv)
	assert.NilError(t, err)
	assert.Equal(t, fi.Mode().Perm(), os.FileMode(0600), "BASH_ENV must be created with mode 0600")
}
