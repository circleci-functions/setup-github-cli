//go:build acceptance

package setupghcli_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"gotest.tools/v3/assert"
)

// acceptanceVersion returns the gh version to install:
// SETUP_GITHUB_CLI_ACCEPTANCE_VERSION is set in CI from the pipeline's
// gh-version parameter, a pinned default locally.
func acceptanceVersion() string {
	if v := os.Getenv("SETUP_GITHUB_CLI_ACCEPTANCE_VERSION"); v != "" {
		return v
	}
	return "2.62.0"
}

// TestAcceptance_Smoke installs gh from a real GitHub release, links it into the
// given bin dir, and checks that binary is the requested version and resolves
// from PATH alone — what a later job step gets, without sourcing anything.
func TestAcceptance_Smoke(t *testing.T) {
	s, run := sandbox(t)
	version := acceptanceVersion()

	out, err := run(t, nil,
		"--version", version,
		"--install-dir", s.InstallDir,
		"--bin-dir", s.BinDir,
	)
	assert.NilError(t, err, "got: %s", out)

	target, err := os.Readlink(filepath.Join(s.BinDir, "gh"))
	assert.NilError(t, err, "expected gh linked into %s; got: %s", s.BinDir, out)
	assert.Assert(t, strings.HasPrefix(target, s.InstallDir),
		"expected the link to point inside %s, got %q", s.InstallDir, target)

	assertGhVersion(t, s.BinDir, version)
	assertResolvesFromPath(t, s.BinDir, version)

	_, statErr := os.Stat(s.BashEnv)
	assert.Assert(t, os.IsNotExist(statErr), "gh goes on PATH by being linked, so $BASH_ENV must be untouched")
}

func assertGhVersion(t *testing.T, ghBinDir, want string) {
	t.Helper()
	out, err := exec.Command(filepath.Join(ghBinDir, "gh"), "--version").CombinedOutput() //nolint:gosec
	assert.NilError(t, err, "gh --version failed: %s", out)
	if want != "latest" {
		assert.Assert(t, strings.Contains(string(out), "gh version "+want),
			"expected gh %s, got: %s", want, out)
	}
}

// assertResolvesFromPath runs `gh --version` with binDir as the whole PATH and
// nothing sourced, the local proxy for what a later step gets.
func assertResolvesFromPath(t *testing.T, binDir, want string) {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), "sh", "-c", "gh --version")
	cmd.Env = append(os.Environ(), "PATH="+binDir)
	out, err := cmd.CombinedOutput()
	assert.NilError(t, err, "resolving gh from PATH alone failed: %s", out)
	if want != "latest" {
		assert.Assert(t, strings.Contains(string(out), "gh version "+want), "got: %s", out)
	}
}
