# setup-github-cli

Install the [GitHub CLI](https://cli.github.com/) (`gh`) in a CircleCI job, and
optionally authenticate it with a Personal Access Token.

## Usage

Invoke it inside a `run:` step with the full function path. Everything after `--`
is passed to the function:

```yaml
jobs:
  release:
    docker:
      - image: cimg/base:stable
    steps:
      - run:
          name: Install the GitHub CLI
          command: circleci run github.com/circleci-functions/setup-github-cli@<version> -- --version 2.62.0
      - run: gh --version
```

Omit `--version`, or set it to `latest`, to install the newest release.

`gh` is made resolvable for later steps by linking it into a directory already on
`PATH`, so it is found in any shell without sourcing anything. It prefers
`~/.local/bin`, then `~/bin`, then `/usr/local/bin`, whichever is on `PATH` and
writable.

A `gh` already on `PATH` doesn't count unless it is the requested version from
this install. The new one is linked ahead of it, or, if no preferred directory
comes first on `PATH`, into that `gh`'s own directory, with the old binary moved
aside (or replaced with `sudo -n` if the directory is root-owned). If neither
works, the step fails rather than leave later steps running a different `gh`.

## Flags

| Flag | Default | Description |
|---|---|---|
| `--version` | `latest` | GitHub CLI version to install: a full `X.Y.Z` release (e.g. `2.62.0`, with or without a leading `v`) or `latest`. |
| `--max-retries` | `5` | Retries when resolving the latest release. |
| `--install-dir` | `~/.cache/setup-github-cli` | Base directory to install `gh` under. |
| `--configure` | `false` | After installing, authenticate `gh` with a token and configure it for git. |
| `--token` | `GITHUB_TOKEN` | Name of the environment variable holding the token. Only used with `--configure`. |
| `--hostname` | `github.com` | GitHub hostname to authenticate against. Only used with `--configure`. |

## Authentication

With `--configure`, the function authenticates `gh` after installing it: it reads
the token from the environment variable named by `--token` (default
`GITHUB_TOKEN`), exports it for later steps, registers `gh` as git's credential
helper, and disables interactive prompts.

```yaml
      - run:
          name: Install and authenticate the GitHub CLI
          command: circleci run github.com/circleci-functions/setup-github-cli@<version> -- --configure
      - run: gh pr list
```

The token must be available in the job — set it as a project or context
environment variable. For **GitHub Enterprise Server**, pass `--hostname`; the
token is then exported as `GH_ENTERPRISE_TOKEN` alongside `GH_HOST`:

```yaml
      - run:
          command: >
            circleci run github.com/circleci-functions/setup-github-cli@<version> --
            --configure --hostname github.example.com --token GHE_TOKEN
```

## Platform support

Linux and macOS, on `amd64` and `arm64`. On any other platform the function exits
with an error rather than installing nothing. macOS `arm64` requires `gh` 2.25.0
or newer.
