package ghcli

import (
	"context"
	"fmt"
	"os"
	"os/exec"

	"github.com/circleci-functions/toolkit/bashenv"
)

// ConfigureOptions configures Configure.
type ConfigureOptions struct {
	// Hostname is the GitHub host to authenticate against, "github.com" or a
	// GitHub Enterprise Server hostname.
	Hostname string
	// TokenEnvVar is the name of the environment variable holding the token.
	TokenEnvVar string
}

// Configure authenticates gh with the token held in the environment variable
// named by opts.TokenEnvVar. It exports the token into $BASH_ENV for later job
// steps, registers gh as git's credential helper, and disables interactive
// prompts. ghBin is the path to the gh binary to drive.
func Configure(ctx context.Context, ghBin string, opts ConfigureOptions) error {
	token := os.Getenv(opts.TokenEnvVar)
	if token == "" {
		return fmt.Errorf("token env var %q is empty", opts.TokenEnvVar)
	}

	exports := tokenExports(opts.Hostname, token)
	if err := bashenv.ExportEnvVars(exports); err != nil {
		return fmt.Errorf("export token: %w", err)
	}

	env := append(os.Environ(), mapToEnv(exports)...)

	if err := runGH(ctx, ghBin, env, "auth", "setup-git", "--hostname", opts.Hostname); err != nil {
		return fmt.Errorf("gh auth setup-git: %w", err)
	}

	if err := runGH(ctx, ghBin, env, "auth", "status"); err != nil {
		fmt.Fprintln(os.Stderr, "setup-github-cli: gh auth status unavailable without proper token scope")
	}

	if err := runGH(ctx, ghBin, env, "config", "set", "prompt", "disabled"); err != nil {
		return fmt.Errorf("gh config set prompt disabled: %w", err)
	}
	return nil
}

// tokenExports returns the environment exports that authenticate gh for hostname
// with token. github.com uses GITHUB_TOKEN; a GitHub Enterprise Server host uses
// GH_ENTERPRISE_TOKEN together with GH_HOST.
func tokenExports(hostname, token string) map[string]string {
	if hostname == "github.com" {
		return map[string]string{"GITHUB_TOKEN": token}
	}
	return map[string]string{
		"GH_ENTERPRISE_TOKEN": token,
		"GH_HOST":             hostname,
	}
}

func mapToEnv(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k, v := range m {
		out = append(out, k+"="+v)
	}
	return out
}

func runGH(ctx context.Context, ghBin string, env []string, args ...string) error {
	cmd := exec.CommandContext(ctx, ghBin, args...)
	cmd.Env = env
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	return cmd.Run()
}
