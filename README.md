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
go run ./cmd/insmith Songmu/gitrail > install.sh
chmod +x install.sh
```

Generated installers support Linux and macOS on amd64 and arm64. They select
`binary_version_os_arch.tar.gz`, `.zip`, or a raw binary, and accept:

```sh
INSTALL_DIR="$HOME/bin" ./install.sh
./install.sh v1.2.3
```

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
attestation aborts installation.

## Synopsis

```go
// simple usage here
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
