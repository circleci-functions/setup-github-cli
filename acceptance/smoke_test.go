//go:build acceptance

package setupghcli_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"gotest.tools/v3/assert"

	"github.com/circleci-functions/toolkit/bashenv"
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

// TestAcceptance_Smoke installs gh from a real GitHub release, then checks the
// binary it put on PATH is the requested version and that a later step sourcing
// $BASH_ENV can resolve it.
func TestAcceptance_Smoke(t *testing.T) {
	s, run := sandbox(t)
	version := acceptanceVersion()

	out, err := run(t, nil,
		"--version", version,
		"--install-dir", s.InstallDir,
		"--bin-dir", s.BinDir,
	)
	assert.NilError(t, err, "got: %s", out)

	ghBinDir := installedGhBinDir(t, s.BashEnv, s.InstallDir)
	assertGhVersion(t, ghBinDir, version)
	assertBashEnvPutsGhOnPath(t, s.BashEnv, ghBinDir)
}

// installedGhBinDir returns the bin directory setup-github-cli prepended to
// PATH, asserting it points inside the sandbox rather than at a preinstalled gh.
func installedGhBinDir(t *testing.T, bashEnvFile, installDir string) string {
	t.Helper()
	path, err := bashenv.Path(bashEnvFile)
	assert.NilError(t, err)

	first, _, _ := strings.Cut(path, string(os.PathListSeparator))
	assert.Assert(t, strings.HasPrefix(first, installDir),
		"expected $BASH_ENV to prepend a bin dir under %s, got %q", installDir, first)
	return first
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

// assertBashEnvPutsGhOnPath sources $BASH_ENV in a real shell and resolves gh
// from it, which is what a later job step does.
func assertBashEnvPutsGhOnPath(t *testing.T, bashEnvFile, wantGhBinDir string) {
	t.Helper()

	script := `set -e; . "$BASH_ENV"; command -v gh`
	cmd := exec.CommandContext(t.Context(), "bash", "-c", script) //nolint:gosec
	cmd.Env = append(os.Environ(), "BASH_ENV="+bashEnvFile)

	out, err := cmd.CombinedOutput()
	assert.NilError(t, err, "sourcing $BASH_ENV and resolving gh failed: %s", out)

	resolved := strings.TrimSpace(string(out))
	assert.Equal(t, filepath.Dir(resolved), wantGhBinDir,
		"expected a later step to resolve gh to the install just made, got %q", resolved)
}
