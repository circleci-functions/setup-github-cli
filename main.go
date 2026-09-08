// Command setup-github-cli installs the GitHub CLI (gh) onto PATH.
//
// Supported platforms: linux and darwin, on amd64 and arm64.
package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/alecthomas/kong"

	"github.com/circleci-functions/toolkit/functionyaml"
	"github.com/circleci-functions/toolkit/sdk"

	"github.com/circleci-functions/setup-github-cli/ghcli"
)

type cli struct {
	Version    string `help:"GitHub CLI version to install (a full semver tag, e.g. 2.37.0) or 'latest'." default:"latest"`
	MaxRetries int    `name:"max-retries" help:"Number of retries when resolving the latest GitHub CLI release." default:"5"`
	InstallDir string `name:"install-dir" help:"Base directory for GitHub CLI installs." default:"~/.cache/setup-github-cli"`
	BinDir     string `name:"bin-dir" help:"Link gh into this directory instead of discovering one on PATH." hidden:"" default:""`
	BaseURL    string `name:"base-url" help:"Override the GitHub base URL releases are fetched from." hidden:"" default:""`
}

func (c *cli) Run(ctx context.Context) error {
	version, err := ghcli.ResolveVersion(ctx, c.BaseURL, c.Version, c.MaxRetries)
	if err != nil {
		return fmt.Errorf("resolve version: %w", err)
	}
	fmt.Fprintf(os.Stderr, "setup-github-cli: resolved %q → %s\n", c.Version, version)

	ghBin, err := ghcli.EnsureInstalled(ctx, version, ghcli.InstallOptions{
		InstallDir: expandHome(c.InstallDir),
		BinDir:     c.BinDir,
		BaseURL:    c.BaseURL,
	})
	if err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "setup-github-cli: gh installed at %s\n", ghBin)
	return nil
}

// expandHome resolves a leading ~ against the home directory, which kong leaves
// literal.
func expandHome(p string) string {
	if p == "~" || strings.HasPrefix(p, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, strings.TrimPrefix(p, "~"))
		}
	}
	return p
}

func main() {
	sdk.Run(run)
}

func run(ctx context.Context, client *sdk.Client) error {
	var flags cli
	kCtx := kong.Parse(&flags,
		kong.Name("setup-github-cli"),
		kong.Description("Install the GitHub CLI (gh).\n\nExample:\n  circleci run github.com/circleci-functions/setup-github-cli@<version> -- --version 2.37.0"),
		kong.UsageOnError(),
		functionyaml.Embed(),
	)
	kCtx.BindTo(ctx, (*context.Context)(nil))
	kCtx.Bind(client)
	return kCtx.Run(ctx)
}
