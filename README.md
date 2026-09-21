insmith
=======

[![Test Status](https://github.com/Songmu/insmith/actions/workflows/test.yaml/badge.svg?branch=main)][actions]
[![Coverage Status](https://codecov.io/gh/Songmu/insmith/branch/main/graph/badge.svg)][codecov]
[![MIT License](https://img.shields.io/github/license/Songmu/insmith)][license]
[![PkgGoDev](https://pkg.go.dev/badge/github.com/Songmu/insmith)][PkgGoDev]

[actions]: https://github.com/Songmu/insmith/actions?workflow=test
[codecov]: https://codecov.io/gh/Songmu/insmith
[license]: https://github.com/Songmu/insmith/blob/main/LICENSE
[PkgGoDev]: https://pkg.go.dev/github.com/Songmu/insmith

`insmith` generates standalone `install.sh` scripts for CLI tools released on
GitHub Releases. Generated installers discover a release asset, install it, and
verify its provenance with GitHub Artifact Attestations or SHA-256 checksums.

## Usage

Generate an installer for a conventional GoReleaser-style release:

```sh
go run ./cmd/insmith --workflow release-build.yaml Songmu/gitrail > install.sh
chmod +x install.sh
```

Generated installers support Linux and macOS on amd64 and arm64. They select
`binary_version_os_arch.tar.gz`, `.zip`, or a raw binary from direct GitHub
Release download URLs, and accept:

```sh
./install.sh -b "$HOME/bin"
BINDIR="$HOME/bin" ./install.sh
./install.sh v1.2.3
./install.sh -d v1.2.3
./install.sh -x -b "$HOME/bin" v1.2.3
```

The default installation directory is `./bin`. Generator flags must precede
the single `OWNER/REPO` argument. Generated installers accept `-d` for shlib
debug logging and `-x` for the shell's command execution trace.

## Options

```text
--binary NAME
--workflow PATH
--asset-pattern PATTERN
--checksum-pattern PATTERN  # default: SHA256SUMS
--verification attestation|attestation-or-checksum|checksum|none
```

Asset patterns may use `{binary}`, `{version}`, `{os}`, and `{arch}`. The
default verification mode is `attestation-or-checksum`: an unavailable
attestation capability falls back to SHA-256 verification, while a failed
attestation or an unresolved release tag aborts installation without
downgrading to checksums.

Generated installers include a fixed excerpt based on `client9/shlib`
v2026.08.30 (`3593994`). It selects the command, logging, platform, archive,
download, GitHub Release, and SHA-256 helpers needed by insmith, while omitting
unselected functions and upstream explanatory comments. The only behavioral
divergence is in `http_download_curl`, where insmith adds `--proto '=https'`
and `--tlsv1.2` to reject plaintext or downgraded transports.

Generated installers require POSIX `sh` and standard Unix tools, and can
download with `curl`, `wget`, `fetch`, `ftp`, Python 3, or Node.js. They
require `tar` or `unzip` for the selected archive format. `git` and a safe
GitHub CLI version with attestation digest support are optional in the default
mode; when unavailable, the installer verifies the release checksum.

## Synopsis

```console
insmith [flags] OWNER/REPO
```

## Description

## Installation

```console
# Install the latest version. (Install it into ./bin/ by default).
% curl -sfL https://raw.githubusercontent.com/Songmu/insmith/main/install.sh | sh -s

# Specify installation directory ($(go env GOPATH)/bin/) and version.
% curl -sfL https://raw.githubusercontent.com/Songmu/insmith/main/install.sh | sh -s -- -b $(go env GOPATH)/bin [vX.Y.Z]

# In alpine linux (as it does not come with curl by default)
% wget -O - -q https://raw.githubusercontent.com/Songmu/insmith/main/install.sh | sh -s [vX.Y.Z]

# go install
% go install github.com/Songmu/insmith/cmd/insmith@latest
```

## Author

[Songmu](https://github.com/Songmu)
