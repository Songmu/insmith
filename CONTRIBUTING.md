# Contributing to insmith

This document collects information for people working on the `insmith`
codebase itself, as opposed to people generating or using an installer script
produced by it.

## Development setup

`insmith` is a Go module; see `go.mod` for the required Go version.

```sh
go build ./...
```

## Testing

```sh
make test   # go test
```

`generate_test.go` and `installer_test.go` cover installer generation and the
behavior of generated scripts (including archive extraction, checksum, and
attestation verification paths) by actually executing the generated
`install.sh` under a shell.

Installer template or header changes must keep `testdata/basic.golden.sh` in
sync: `TestGenerateGolden` in `generate_test.go` compares a freshly generated
script byte-for-byte against that golden file. Regenerate it whenever
`install.sh.tmpl` or `install.sh.header` changes, and review the diff
carefully before committing it.

## Building

```sh
make build     # go build ./cmd/insmith
make install   # go install ./cmd/insmith
```

## Release process

```sh
make prepare-release
```

This runs `go get`/`go mod tidy`, regenerates `CREDITS` via
`gocredits -w` (installed by `make devel-deps`), and stages the resulting
files for the release commit.

## Implementation notes

### Generated installer implementation

Generated installers embed a fixed excerpt based on `client9/shlib`
v2026.08.30 (`3593994`). Only the command, logging, platform, archive,
download, GitHub Release, and SHA-256 helpers needed by insmith are included;
unselected functions and upstream explanatory comments are omitted. The only
behavioral divergence from upstream is in `http_download_curl`, where insmith
adds `--proto '=https'` and `--tlsv1.2` to reject plaintext or downgraded
transports.

The generated script also retains attribution to `goreleaser/godownloader`,
which the installer's base and ideas come from.

### Shell portability

Generated installers use `#!/bin/sh` and target Linux, macOS, and Windows
(under a POSIX-compatible shell), so embedded shell code in `install.sh.tmpl`
and `install.sh.header` must stay POSIX-compatible.

### Archive extraction safety

The generated installer validates `tar.gz`/`zip` members (rejecting absolute
paths, `../` traversal, and symlinks) before extracting into a dedicated
`tmpdir/extract` subdirectory. See `install.sh.tmpl` and the corresponding
cases in `installer_test.go`.
