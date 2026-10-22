# setup-github-cli

Install the [GitHub CLI](https://cli.github.com/) (`gh`) in a CircleCI job, and
optionally authenticate it with a Personal Access Token.

## Usage

Declare the function in a top-level `functions:` block, then call it as a step.
Each flag in the table below is a `with:` key, written without its leading `--`:

```yaml
functions:
  setup-github-cli: github.com/circleci-functions/setup-github-cli@<version>

jobs:
  release:
    docker:
      - image: cimg/base:stable
    steps:
      - setup-github-cli:
          with:
            version: 2.62.0
      - run: gh --version
```

`circleci function add setup-github-cli` writes the `functions:` entry for you.

Where the `functions:` block isn't enabled, such as on CircleCI Server, run it
with `circleci run` and the function's full name. Everything after `--` is passed
to the function:

```yaml
      - run: circleci run github.com/circleci-functions/setup-github-cli@<version> -- --version 2.62.0
```

Omit `--version`, or set it to `latest`, to install the newest release.

Before extracting, the function checks the downloaded archive against the SHA-256
listed in the release's `gh_<version>_checksums.txt`, and fails on a mismatch.
This catches a download corrupted or altered in transit, not a compromised
release.

`gh` is made resolvable for later steps by linking it into a directory already on
`PATH`, so it is found in any shell without sourcing anything. It prefers
`~/.local/bin`, then `~/bin`, then `/usr/local/bin`, whichever is on `PATH` and
writable.

A `gh` already on `PATH` doesn't count unless it is the requested version from
this install. The new one is linked ahead of it, or, if no preferred directory
comes first on `PATH`, into that `gh`'s own directory, with the old binary moved
aside (or replaced with `sudo -n` if the directory is root-owned). If neither
works, the step fails rather than leave later steps running a different `gh`.

## How it gets onto PATH

A step inherits its environment from the job, not from the step before it, so
setup-github-cli cannot just prepend to `PATH`. It links `gh` into a directory
`PATH` already holds, which reaches every later process whatever shell it runs
in.

The directory is chosen by preference from writable directories already on
`PATH`. If an older `gh` is already installed in that directory, setup-github-cli
replaces the link and keeps the old binary as `gh.pathlink-old` in the same
directory. If the chosen directory is not writable, setup-github-cli falls back
to `sudo -n ln`; on macOS system directories, sudo is skipped.

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
helper, and disables interactive prompts. The export is a plaintext
`export GITHUB_TOKEN=<token>` line appended to `$BASH_ENV` (`GH_ENTERPRISE_TOKEN`
for GitHub Enterprise Server), so anything later in the job that can read that
file can read the token.

```yaml
      - setup-github-cli:
          with:
            configure: true
      - run: gh pr list
```

The token must be available in the job — set it as a project or context
environment variable. If that variable is unset or empty, the step fails before
installing anything. For **GitHub Enterprise Server**, pass `--hostname`; the
token is then exported as `GH_ENTERPRISE_TOKEN` alongside `GH_HOST`:

```yaml
      - setup-github-cli:
          with:
            configure: true
            hostname: github.example.com
            token: GHE_TOKEN
```

## Platform support

Linux and macOS, on `amd64` and `arm64`. On any other platform the function exits
with an error rather than installing nothing. macOS `arm64` requires `gh` 2.25.0
or newer.
