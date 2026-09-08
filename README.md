# setup-github-cli

Install the [GitHub CLI](https://cli.github.com/) (`gh`) in a CircleCI job.

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
`PATH` (`~/.local/bin`, `~/bin`, or `/usr/local/bin`), so it is found in any
shell without sourcing anything.

## Flags

| Flag | Default | Description |
|---|---|---|
| `--version` | `latest` | GitHub CLI version to install: a full semver tag (e.g. `2.62.0`) or `latest`. |
| `--max-retries` | `5` | Retries when resolving the latest release. |
| `--install-dir` | `~/.cache/setup-github-cli` | Base directory to install `gh` under. |

## Platform support

Linux and macOS, on `amd64` and `arm64`. On any other platform the function exits
with an error rather than installing nothing. macOS `arm64` requires `gh` 2.25.0
or newer.
