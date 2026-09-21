package insmith

import (
	"bytes"
	"context"
	"os"
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
		`main "$@"`,
		`BINDIR=${BINDIR:-./bin}`,
		`while getopts "b:h" arg`,
		`git ls-remote "https://github.com/$REPOSITORY.git"`,
		"gh attestation verify",
		`--source-digest "$commit"`,
		`--signer-digest "$commit"`,
		"--proto '=https' --proto-redir '=https' --tlsv1.2",
		"trap cleanup 0",
		"trap 'exit 1' HUP INT TERM",
		`""|*[!A-Za-z0-9._+-]*) return 1`,
		"tar_names=$(tar -tzf \"$artifact\")",
		"h*) fail \"archive contains a hard link entry which is not supported\"",
		"verify_checksum",
		`install -d "$BINDIR"`,
	} {
		if !strings.Contains(script, want) {
			t.Errorf("generated script does not contain %q", want)
		}
	}
}

func TestGenerateGolden(t *testing.T) {
	script, err := generate(config{
		repository:      "Songmu/gitrail",
		workflow:        "release-build.yaml",
		checksumPattern: "SHA256SUMS",
		verification:    "attestation-or-checksum",
	})
	if err != nil {
		t.Fatal(err)
	}
	want, err := os.ReadFile("testdata/basic.golden.sh")
	if err != nil {
		t.Fatal(err)
	}
	if script != string(want) {
		t.Error("generated installer differs from testdata/basic.golden.sh")
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
	for _, pattern := range []string{"{unknown}", "{binary", "binary}"} {
		if _, err := generate(config{repository: "owner/repo", assetPattern: pattern, verification: "none"}); err == nil {
			t.Errorf("generate accepted an invalid asset pattern %q", pattern)
		}
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
	if err := runGenerator([]string{"-h"}, &stdout, &stderr); err != nil {
		t.Fatalf("run help: %v", err)
	}
	if !strings.Contains(stderr.String(), "Usage: insmith [flags] OWNER/REPO") {
		t.Errorf("help output = %q", stderr.String())
	}
}

func TestRunGenerate(t *testing.T) {
	args := []string{"--verification=none", "owner/repo"}
	var stdout, stderr bytes.Buffer
	if err := Run(context.Background(), args, &stdout, &stderr); err != nil {
		t.Fatalf("Run(%q): %v", args, err)
	}
	if !strings.Contains(stdout.String(), "REPOSITORY='owner/repo'") {
		t.Errorf("Run(%q) output = %q", args, stdout.String())
	}
}

func TestRunRequiresFlagsBeforeRepository(t *testing.T) {
	var stdout, stderr bytes.Buffer
	err := Run(context.Background(), []string{"owner/repo", "--verification=none"}, &stdout, &stderr)
	if err == nil || !strings.Contains(err.Error(), "flags must precede") {
		t.Fatalf("Run() error = %v", err)
	}
}

func TestRunVersion(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if err := Run(context.Background(), []string{"-version"}, &stdout, &stderr); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(stdout.String(), "insmith v") {
		t.Errorf("version output = %q", stdout.String())
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
