# insmith

`insmith` generates standalone `install.sh` scripts for CLI tools released on
GitHub Releases. Generated installers discover a release asset, install it, and
verify its provenance with GitHub Artifact Attestations or SHA-256 checksums.

## Usage

Generate an installer for a conventional GoReleaser-style release:

```sh
go run . generate Songmu/gitrail > install.sh
chmod +x install.sh
```

The command shorthand is also supported:

```sh
go run . Songmu/gitrail > install.sh
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
--checksum-pattern PATTERN
--verification attestation|attestation-or-checksum|checksum|none
```

Asset patterns may use `{binary}`, `{version}`, `{os}`, and `{arch}`. The
default verification mode is `attestation-or-checksum`: an unavailable
attestation capability falls back to SHA-256 verification, while a failed
attestation aborts installation.