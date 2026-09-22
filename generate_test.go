package insmith

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"testing"
)

func TestGenerateDefaults(t *testing.T) {
	script, err := generate(config{repository: "Songmu/gitrail", workflow: "release-build.yaml", checksumPattern: "SHA256SUMS", verification: "attestation-or-checksum"})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"The base and ideas of this installer come from godownloader.",
		"https://github.com/goreleaser/godownloader",
		"REPOSITORY='Songmu/gitrail'",
		"NAME='gitrail'",
		"BINARIES='gitrail'",
		"ASSET_PATTERN='{name}_{version}_{os}_{arch}'",
		"WORKFLOW='Songmu/gitrail/.github/workflows/release-build.yaml'",
		`main "$@"`,
		`BINDIR=${BINDIR:-./bin}`,
		`while getopts "b:dhx" arg`,
		`d) log_set_priority 10`,
		`x) set -x`,
		`git ls-remote "https://github.com/$REPOSITORY.git"`,
		"gh attestation verify",
		`--source-digest "$commit"`,
		`--signer-digest "$commit"`,
		"--proto '=https' --tlsv1.2",
		"Selected functions based on client9/shlib v2026.08.30 (commit 3593994).",
		"Unselected functions and upstream explanatory comments are omitted.",
		"The only behavioral divergence from v2026.08.30 is in http_download_curl",
		`TAG=$(github_release "$REPOSITORY" "$requested_tag")`,
		`GITHUB_DOWNLOAD="https://github.com/$REPOSITORY/releases/download"`,
		`darwin|windows) extensions='.zip .tar.gz .exe raw'`,
		`windows/amd64|windows/arm64`,
		`platform_binary_name`,
		"trap cleanup 0",
		"trap 'exit 1' HUP INT TERM",
		`*[!A-Za-z0-9._+-]* | "")`,
		"tar_names=$(tar -tzf \"$artifact\")",
		`*\\*) fail "archive contains a Windows path separator: $member"`,
		`[A-Za-z]:*) fail "archive contains a drive-qualified path: $member"`,
		"h*) fail \"archive contains a hard link entry which is not supported\"",
		"verify_checksum",
		`install -d "$BINDIR"`,
		`install -m 0755 "$executable" "$install_tmp"`,
		`mv -f "$install_tmp" "$install_destination"`,
	} {
		if !strings.Contains(script, want) {
			t.Errorf("generated script does not contain %q", want)
		}
	}
	if strings.Contains(script, "api.github.com") {
		t.Error("generated script uses the GitHub API")
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

func TestGenerateVerificationSpecialization(t *testing.T) {
	shell, err := exec.LookPath("sh")
	if err != nil {
		t.Skip("sh is not available")
	}
	tests := []struct {
		verification string
		present      []string
		absent       []string
	}{
		{
			verification: "none",
			absent: []string{
				"VERIFICATION=", "CHECKSUM_PATTERN=", "WORKFLOW=", "verify_artifact",
				"verify_checksum", "verify_attestation", "hash_sha256",
			},
		},
		{
			verification: "checksum",
			present:      []string{"CHECKSUM_PATTERN=", "verify_checksum", "hash_sha256"},
			absent:       []string{"VERIFICATION=", "WORKFLOW=", "verify_attestation", "attestation_capability"},
		},
		{
			verification: "attestation",
			present:      []string{"WORKFLOW=", "verify_attestation", "attestation_capability"},
			absent:       []string{"VERIFICATION=", "CHECKSUM_PATTERN=", "verify_checksum", "hash_sha256"},
		},
		{
			verification: "attestation-or-checksum",
			present:      []string{"WORKFLOW=", "CHECKSUM_PATTERN=", "verify_checksum", "verify_attestation", "hash_sha256"},
			absent:       []string{"VERIFICATION="},
		},
	}
	for _, tt := range tests {
		t.Run(tt.verification, func(t *testing.T) {
			script, err := generate(config{
				repository:      "owner/repo",
				checksumPattern: "SHA256SUMS",
				verification:    tt.verification,
			})
			if err != nil {
				t.Fatal(err)
			}
			for _, text := range tt.present {
				if !strings.Contains(script, text) {
					t.Errorf("generated script does not contain %q", text)
				}
			}
			for _, text := range tt.absent {
				if strings.Contains(script, text) {
					t.Errorf("generated script unexpectedly contains %q", text)
				}
			}
			cmd := exec.Command(shell, "-n")
			cmd.Stdin = strings.NewReader(script)
			if output, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("generated script is invalid: %v\n%s", err, output)
			}
		})
	}
}

func TestGenerateValidation(t *testing.T) {
	type testCase struct {
		name string
		cfg  config
	}
	tests := []testCase{
		{"invalid repository", config{repository: "invalid", verification: "none"}},
		{"invalid verification policy", config{repository: "owner/repo", verification: "unknown"}},
		{"unsafe name", config{repository: "owner/repo", name: "bad/name", verification: "none"}},
		{"unsafe binary name", config{repository: "owner/repo", binaries: []string{"good", "bad/name"}, verification: "none"}},
		{"duplicate binary names", config{repository: "owner/repo", binaries: []string{"duplicate", "duplicate"}, verification: "none"}},
		{"unsafe checksum pattern", config{repository: "owner/repo", checksumPattern: "bad/path", verification: "checksum"}},
	}
	for _, pattern := range []string{"{unknown}", "{binary}", "{name", "name}"} {
		tests = append(tests, testCase{
			name: "invalid asset pattern " + pattern,
			cfg:  config{repository: "owner/repo", assetPattern: pattern, verification: "none"},
		})
	}
	for _, repository := range []string{"owner/repo?foo=bar", "owner/repo#fragment", "own?er/repo", "owner/repo/name", "./repo", "../repo", "owner/.", "owner/.."} {
		tests = append(tests, testCase{
			name: "invalid repository " + repository,
			cfg:  config{repository: repository, verification: "none"},
		})
	}
	for _, name := range []string{".", ".."} {
		tests = append(tests,
			testCase{name: "unsafe name " + name, cfg: config{repository: "owner/repo", name: name, verification: "none"}},
			testCase{name: "unsafe binary name " + name, cfg: config{repository: "owner/repo", binaries: []string{name}, verification: "none"}},
		)
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := generate(tt.cfg); err == nil {
				t.Errorf("generate accepted config %+v", tt.cfg)
			}
		})
	}
}

func TestShellQuote(t *testing.T) {
	if got, want := shellQuote("a'b"), `'a'"'"'b'`; got != want {
		t.Errorf("shellQuote() = %q, want %q", got, want)
	}
}

func TestRunHelp(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if err := runGenerator(context.Background(), []string{"-h"}, &stdout, &stderr); err != nil {
		t.Fatalf("run help: %v", err)
	}
	if !strings.Contains(stderr.String(), "Usage: insmith [flags] OWNER/REPO") {
		t.Errorf("help output = %q", stderr.String())
	}
}

func TestRunGenerate(t *testing.T) {
	args := []string{"--verification=none", "--name=tools", "--binary=foo", "--binary=bar", "owner/repo"}
	var stdout, stderr bytes.Buffer
	if err := Run(context.Background(), args, &stdout, &stderr); err != nil {
		t.Fatalf("Run(%q): %v", args, err)
	}
	if !strings.Contains(stdout.String(), "NAME='tools'") ||
		!strings.Contains(stdout.String(), "BINARIES='foo bar'") {
		t.Errorf("Run(%q) output = %q", args, stdout.String())
	}
}

func TestRunVerificationDefaults(t *testing.T) {
	mockWorkflowAPI(t, "/repos/owner/repo/contents/.github/workflows/release-build.yaml", http.StatusOK)
	var stdout, stderr bytes.Buffer
	if err := Run(context.Background(), []string{"owner/repo"}, &stdout, &stderr); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"WORKFLOW='owner/repo/.github/workflows/release-build.yaml'",
		"CHECKSUM_PATTERN='SHA256SUMS'",
	} {
		if !strings.Contains(stdout.String(), want) {
			t.Errorf("default output does not contain %q", want)
		}
	}
}

func TestRunMissingDefaultWorkflowDisablesPinning(t *testing.T) {
	mockWorkflowAPI(t, "/repos/owner/repo/contents/.github/workflows/release-build.yaml", http.StatusNotFound)
	var stdout, stderr bytes.Buffer
	if err := Run(context.Background(), []string{"owner/repo"}, &stdout, &stderr); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout.String(), "WORKFLOW=''") {
		t.Errorf("missing default workflow output = %q", stdout.String())
	}
}

func TestRunMissingExplicitWorkflowFails(t *testing.T) {
	mockWorkflowAPI(t, "/repos/owner/repo/contents/.github/workflows/release.yaml", http.StatusNotFound)
	var stdout, stderr bytes.Buffer
	err := Run(context.Background(), []string{"--workflow=release.yaml", "owner/repo"}, &stdout, &stderr)
	if err == nil || !strings.Contains(err.Error(), `workflow "owner/repo/.github/workflows/release.yaml" does not exist`) {
		t.Fatalf("Run() error = %v", err)
	}
}

func TestRunEmptyWorkflowDisablesPinning(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if err := Run(context.Background(), []string{"--workflow=", "owner/repo"}, &stdout, &stderr); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout.String(), "WORKFLOW=''") {
		t.Errorf("empty workflow output = %q", stdout.String())
	}
}

func mockWorkflowAPI(t *testing.T, expectedPath string, status int) {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != expectedPath {
			t.Errorf("workflow API path = %q, want %q", r.URL.Path, expectedPath)
		}
		w.WriteHeader(status)
		_, _ = fmt.Fprintln(w, "{}")
	}))
	t.Cleanup(server.Close)
	oldBaseURL := githubAPIBaseURL
	githubAPIBaseURL = server.URL
	t.Cleanup(func() {
		githubAPIBaseURL = oldBaseURL
	})
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
			verification: "attestation",
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
