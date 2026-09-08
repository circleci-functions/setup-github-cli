// Command setup-github-cli installs the GitHub CLI (gh) and, with --configure,
// authenticates it with a Personal Access Token and sets it up for use in later
// steps by exporting the token into $BASH_ENV and configuring the git credential
// helper.
//
// Supported platforms: linux and darwin, on amd64 and arm64.
package main

import (
	"context"

	"github.com/alecthomas/kong"

	"github.com/circleci-functions/toolkit/functionyaml"
	"github.com/circleci-functions/toolkit/sdk"
)

type cli struct {
	Version    string `help:"GitHub CLI version to install (a full semver tag, e.g. 2.37.0) or 'latest'." default:"latest"`
	MaxRetries int    `help:"Number of retries when resolving the latest GitHub CLI release." default:"5"`
	Configure  bool   `help:"After installing, authenticate gh with a Personal Access Token and configure it for git." default:"false"`
	Token      string `help:"Name of the environment variable holding the GitHub token. Only used with --configure." default:"GITHUB_TOKEN"`
	Hostname   string `help:"GitHub hostname to authenticate against. Only used with --configure." default:"github.com"`
}

func (c *cli) Run(ctx context.Context, client *sdk.Client) error {
	return nil
}

func main() {
	sdk.Run(run)
}

func run(ctx context.Context, client *sdk.Client) error {
	var flags cli
	kCtx := kong.Parse(&flags,
		kong.Name("setup-github-cli"),
		kong.Description("Install the GitHub CLI (gh), optionally authenticating it with a Personal Access Token.\n\nExample:\n  circleci run github.com/circleci-functions/setup-github-cli@<version> -- --version 2.37.0"),
		kong.UsageOnError(),
		functionyaml.Embed(),
	)
	kCtx.BindTo(ctx, (*context.Context)(nil))
	kCtx.Bind(client)
	return kCtx.Run(ctx)
}
