package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestGenerateDefaults(t *testing.T) {
	script, err := generate(config{repository: "Songmu/gitrail", workflow: "release-build.yaml", checksumPattern: "SHA256SUMS", verification: "attestation-or-checksum"})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"REPOSITORY='Songmu/gitrail'",
		"BINARY='gitrail'",
		"ASSET_PATTERN='{binary}_{version}_{os}_{arch}'",
		"WORKFLOW='Songmu/gitrail/.github/workflows/release-build.yaml'",
		"git ls-remote",
		"gh attestation verify",
		"--proto '=https' --tlsv1.2",
		"verify_checksum",
		"INSTALL_DIR=${INSTALL_DIR:-/usr/local/bin}",
	} {
		if !strings.Contains(script, want) {
			t.Errorf("generated script does not contain %q", want)
		}
	}
}

func TestGenerateValidation(t *testing.T) {
	_, err := generate(config{repository: "invalid", verification: "none"})
	if err == nil {
		t.Error("generate accepted an invalid repository")
	}
	_, err = generate(config{repository: "owner/repo", verification: "unknown"})
	if err == nil {
		t.Error("generate accepted an invalid verification policy")
	}
}

func TestShellQuote(t *testing.T) {
	if got, want := shellQuote("a'b"), `'a'"'"'b'`; got != want {
		t.Errorf("shellQuote() = %q, want %q", got, want)
	}
}

func TestRunHelp(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if err := run([]string{"generate", "-h"}, &stdout, &stderr); err != nil {
		t.Fatalf("run help: %v", err)
	}
	if !strings.Contains(stderr.String(), "Usage of insmith generate:") {
		t.Errorf("help output = %q", stderr.String())
	}
}
