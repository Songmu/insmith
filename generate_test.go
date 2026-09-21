package insmith

import (
	"bytes"
	"context"
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
		"gh attestation verify \"$artifact\" --repo \"$REPOSITORY\" --source-digest \"$commit\" --signer-digest \"$commit\" || return 1",
		"--proto '=https' --proto-redir '=https' --tlsv1.2",
		"trap 'rm -rf \"$tmpdir\"' 0",
		"trap 'exit 1' HUP INT TERM",
		"case \"$1\" in *[!A-Za-z0-9._+-]*) return 1 ;; esac",
		"tar_names=$(tar -tzf \"$artifact\")",
		"h*) fail \"archive contains a hard link entry which is not supported\"",
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
	_, err = generate(config{repository: "owner/repo", binary: "bad/name", verification: "none"})
	if err == nil {
		t.Error("generate accepted an unsafe binary name")
	}
	_, err = generate(config{repository: "owner/repo", checksumPattern: "bad/path", verification: "checksum"})
	if err == nil {
		t.Error("generate accepted an unsafe checksum pattern")
	}
	for _, repository := range []string{"owner/repo?foo=bar", "owner/repo#fragment", "own?er/repo", "owner/repo/name", "./repo", "../repo", "owner/.", "owner/.."} {
		if _, err := generate(config{repository: repository, verification: "none"}); err == nil {
			t.Errorf("generate accepted an invalid repository %q", repository)
		}
	}
	for _, binary := range []string{".", ".."} {
		if _, err := generate(config{repository: "owner/repo", binary: binary, verification: "none"}); err == nil {
			t.Errorf("generate accepted an unsafe binary name %q", binary)
		}
	}
}

func TestShellQuote(t *testing.T) {
	if got, want := shellQuote("a'b"), `'a'"'"'b'`; got != want {
		t.Errorf("shellQuote() = %q, want %q", got, want)
	}
}

func TestRunHelp(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if err := runGenerate([]string{"-h"}, &stdout, &stderr); err != nil {
		t.Fatalf("run help: %v", err)
	}
	if !strings.Contains(stderr.String(), "Usage of insmith generate:") {
		t.Errorf("help output = %q", stderr.String())
	}
}

func TestRunGenerate(t *testing.T) {
	for _, args := range [][]string{
		{"generate", "--verification=none", "owner/repo"},
		{"--verification=none", "owner/repo"},
	} {
		var stdout, stderr bytes.Buffer
		if err := Run(context.Background(), args, &stdout, &stderr); err != nil {
			t.Fatalf("Run(%q): %v", args, err)
		}
		if !strings.Contains(stdout.String(), "REPOSITORY='owner/repo'") {
			t.Errorf("Run(%q) output = %q", args, stdout.String())
		}
	}
}

func TestWorkflowPath(t *testing.T) {
	for _, workflow := range []string{"release.yaml", ".github/workflows/release.yaml", "owner/repo/.github/workflows/release.yaml", "other/repo/.github/workflows/release.yaml"} {
		script, err := generate(config{
			repository:   "owner/repo",
			workflow:     workflow,
			verification: "none",
		})
		if err != nil {
			t.Fatal(err)
		}
		want := "WORKFLOW='owner/repo/.github/workflows/release.yaml'"
		if strings.HasPrefix(workflow, "other/") {
			want = "WORKFLOW='other/repo/.github/workflows/release.yaml'"
		}
		if !strings.Contains(script, want) {
			t.Errorf("workflow %q generated an unqualified path", workflow)
		}
	}
}
