package insmith

import (
	"bytes"
	"context"
	_ "embed"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"text/template"
	"time"
)

type config struct {
	repository      string
	name            string
	binaries        []string
	workflow        string
	assetPattern    string
	checksumPattern string
	verification    string
}

const (
	verificationNone                  = "none"
	verificationChecksum              = "checksum"
	verificationAttestation           = "attestation"
	verificationAttestationOrChecksum = "attestation-or-checksum"
)

var githubAPIBaseURL = "https://api.github.com"
var workflowHTTPClient = &http.Client{Timeout: 10 * time.Second}

type stringListFlag []string

type repositoryContext struct {
	name string
	root string
}

func (f *stringListFlag) String() string {
	return strings.Join(*f, ",")
}

func (f *stringListFlag) Set(value string) error {
	*f = append(*f, value)
	return nil
}

func runGenerator(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("insmith", flag.ContinueOnError)
	flags.SetOutput(stderr)
	flags.Usage = func() {
		fmt.Fprintln(stderr, "Usage: insmith [flags] [OWNER/REPO]")
		fmt.Fprintln(stderr)
		flags.PrintDefaults()
	}
	name := flags.String("name", "", "release package name (defaults to repository name)")
	var binaries stringListFlag
	flags.Var(&binaries, "binary", "binary to install (repeatable; defaults to name)")
	workflow := flags.String("workflow", "release-build.yaml", "release workflow path for attestation verification (empty disables workflow pinning)")
	assetPattern := flags.String("asset-pattern", "", "asset name pattern using {name}, {version}, {os}, and {arch}")
	checksumPattern := flags.String("checksum-pattern", "SHA256SUMS", "checksum asset name pattern")
	verification := flags.String("verification", verificationAttestationOrChecksum, "verification policy: attestation, attestation-or-checksum, checksum, or none")
	showVersion := flags.Bool("version", false, "display version")
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if *showVersion {
		return printVersion(stdout)
	}
	if flags.NArg() > 1 {
		flags.Usage()
		return errors.New("at most one OWNER/REPO argument is allowed; flags must precede it")
	}
	repository, err := resolveRepository(ctx, flags.Args())
	if err != nil {
		return err
	}

	workflowExplicit := false
	flags.Visit(func(f *flag.Flag) {
		if f.Name == "workflow" {
			workflowExplicit = true
		}
	})
	resolvedWorkflow := *workflow
	if workflowExplicit || *verification == verificationAttestation || *verification == verificationAttestationOrChecksum {
		resolvedWorkflow, err = resolveWorkflow(ctx, repository, resolvedWorkflow, workflowExplicit)
		if err != nil {
			return err
		}
	}

	script, err := generate(config{
		repository:      repository.name,
		name:            *name,
		binaries:        binaries,
		workflow:        resolvedWorkflow,
		assetPattern:    *assetPattern,
		checksumPattern: *checksumPattern,
		verification:    *verification,
	})
	if err != nil {
		return err
	}
	_, err = io.WriteString(stdout, script)
	return err
}

func generate(c config) (string, error) {
	if err := validateRepository(c.repository); err != nil {
		return "", err
	}
	parts := strings.Split(c.repository, "/")
	if c.name == "" {
		c.name = parts[1]
	}
	if !safeFilename(c.name) || c.name == "." || c.name == ".." {
		return "", fmt.Errorf("name must contain only letters, digits, dots, underscores, and hyphens")
	}
	if len(c.binaries) == 0 {
		c.binaries = []string{c.name}
	}
	seenBinaries := make(map[string]struct{}, len(c.binaries))
	for _, binary := range c.binaries {
		if !safeFilename(binary) || binary == "." || binary == ".." {
			return "", fmt.Errorf("binary %q must contain only letters, digits, dots, underscores, and hyphens", binary)
		}
		if _, ok := seenBinaries[binary]; ok {
			return "", fmt.Errorf("binary %q is specified more than once", binary)
		}
		seenBinaries[binary] = struct{}{}
	}
	if c.assetPattern == "" {
		c.assetPattern = "{name}_{version}_{os}_{arch}"
	}
	if !safePattern(c.assetPattern) {
		return "", fmt.Errorf("asset pattern contains unsupported characters")
	}
	if c.checksumPattern != "" && !safePattern(c.checksumPattern) {
		return "", fmt.Errorf("checksum pattern contains unsupported characters")
	}
	c.workflow = qualifyWorkflow(c.repository, c.workflow)
	switch c.verification {
	case verificationAttestation, verificationAttestationOrChecksum, verificationChecksum, verificationNone:
	default:
		return "", fmt.Errorf("unknown verification policy %q", c.verification)
	}
	if c.checksumPattern == "" && c.verification != verificationNone && c.verification != verificationAttestation {
		return "", fmt.Errorf("checksum pattern must not be empty when checksum verification is enabled")
	}

	var output bytes.Buffer
	if err := scriptTemplate.Execute(&output, struct {
		Repository              string
		Name                    string
		Binaries                string
		Workflow                string
		AssetPattern            string
		ChecksumPattern         string
		ChecksumVerification    bool
		AttestationVerification bool
		WorkflowPinning         bool
	}{
		Repository:              shellQuote(c.repository),
		Name:                    shellQuote(c.name),
		Binaries:                shellQuote(strings.Join(c.binaries, " ")),
		Workflow:                shellQuote(c.workflow),
		AssetPattern:            shellQuote(c.assetPattern),
		ChecksumPattern:         shellQuote(c.checksumPattern),
		ChecksumVerification:    c.verification == verificationChecksum || c.verification == verificationAttestationOrChecksum,
		AttestationVerification: c.verification == verificationAttestation || c.verification == verificationAttestationOrChecksum,
		WorkflowPinning:         c.workflow != "" && (c.verification == verificationAttestation || c.verification == verificationAttestationOrChecksum),
	}); err != nil {
		return "", err
	}
	return output.String(), nil
}

func validateRepository(repository string) error {
	parts := strings.Split(repository, "/")
	if len(parts) != 2 || !safeFilename(parts[0]) || !safeFilename(parts[1]) ||
		parts[0] == "." || parts[0] == ".." || parts[1] == "." || parts[1] == ".." {
		return fmt.Errorf("repository must be in OWNER/REPO form")
	}
	return nil
}

func resolveRepository(ctx context.Context, args []string) (repositoryContext, error) {
	if len(args) == 1 {
		if err := validateRepository(args[0]); err != nil {
			return repositoryContext{}, err
		}
		return repositoryContext{name: args[0]}, nil
	}
	root, err := gitOutput(ctx, ".", "rev-parse", "--show-toplevel")
	if err != nil {
		return repositoryContext{}, fmt.Errorf("resolve local repository root: %w", err)
	}
	remote, err := gitOutput(ctx, root, "remote", "get-url", "origin")
	if err != nil {
		return repositoryContext{}, fmt.Errorf("resolve local repository origin: %w", err)
	}
	repository, err := githubRepositoryFromRemote(remote)
	if err != nil {
		return repositoryContext{}, err
	}
	return repositoryContext{name: repository, root: root}, nil
}

func gitOutput(ctx context.Context, directory string, args ...string) (string, error) {
	commandArgs := append([]string{"-C", directory}, args...)
	output, err := exec.CommandContext(ctx, "git", commandArgs...).CombinedOutput()
	if err != nil {
		message := strings.TrimSpace(string(output))
		if message == "" {
			message = err.Error()
		}
		return "", errors.New(message)
	}
	return strings.TrimSpace(string(output)), nil
}

func githubRepositoryFromRemote(remote string) (string, error) {
	remote = strings.TrimSpace(remote)
	var repository string
	if strings.HasPrefix(remote, "git@github.com:") {
		repository = strings.TrimPrefix(remote, "git@github.com:")
	} else {
		parsed, err := url.Parse(remote)
		if err != nil || !strings.EqualFold(parsed.Hostname(), "github.com") {
			return "", fmt.Errorf("origin must point to a github.com repository")
		}
		repository = strings.TrimPrefix(parsed.Path, "/")
	}
	repository = strings.TrimSuffix(strings.TrimSuffix(repository, "/"), ".git")
	if err := validateRepository(repository); err != nil {
		return "", fmt.Errorf("invalid GitHub origin %q: %w", remote, err)
	}
	return repository, nil
}

func resolveWorkflow(ctx context.Context, repository repositoryContext, workflow string, explicit bool) (string, error) {
	workflow = qualifyWorkflow(repository.name, workflow)
	if workflow == "" {
		return "", nil
	}
	exists, err := workflowExists(ctx, repository, workflow)
	if err != nil {
		return "", err
	}
	if exists {
		return workflow, nil
	}
	if explicit {
		return "", fmt.Errorf("workflow %q does not exist", workflow)
	}
	return "", nil
}

func qualifyWorkflow(repository, workflow string) string {
	if workflow == "" || strings.Contains(workflow, "/.github/workflows/") {
		return workflow
	}
	workflow = strings.TrimPrefix(workflow, "/")
	if !strings.HasPrefix(workflow, ".github/workflows/") {
		workflow = ".github/workflows/" + workflow
	}
	return repository + "/" + workflow
}

func workflowExists(ctx context.Context, localRepository repositoryContext, workflow string) (bool, error) {
	repository, workflowName, err := splitWorkflow(workflow)
	if err != nil {
		return false, err
	}
	if localRepository.root != "" && repository == localRepository.name {
		return localWorkflowExists(localRepository.root, workflow, workflowName)
	}
	return remoteWorkflowExists(ctx, workflow, repository, workflowName)
}

func splitWorkflow(workflow string) (string, string, error) {
	repository, workflowName, ok := strings.Cut(workflow, "/.github/workflows/")
	if !ok || repository == "" || workflowName == "" {
		return "", "", fmt.Errorf("invalid workflow path %q", workflow)
	}
	return repository, workflowName, nil
}

func localWorkflowExists(root, workflow, workflowName string) (bool, error) {
	workflowDirectory := filepath.Join(root, ".github", "workflows")
	workflowPath := filepath.Join(workflowDirectory, filepath.FromSlash(workflowName))
	relative, err := filepath.Rel(workflowDirectory, workflowPath)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return false, fmt.Errorf("invalid workflow path %q", workflow)
	}
	info, err := os.Stat(workflowPath)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("check workflow %q: %w", workflow, err)
	}
	return info.Mode().IsRegular(), nil
}

func remoteWorkflowExists(ctx context.Context, workflow, repository, workflowName string) (bool, error) {
	endpoint := githubAPIBaseURL + "/repos/" + escapePath(repository) +
		"/contents/.github/workflows/" + escapePath(workflowName)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return false, fmt.Errorf("check workflow %q: %w", workflow, err)
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "insmith")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	token := os.Getenv("GH_TOKEN")
	if token == "" {
		token = os.Getenv("GITHUB_TOKEN")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := workflowHTTPClient.Do(req)
	if err != nil {
		return false, fmt.Errorf("check workflow %q: %w", workflow, err)
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	switch {
	case resp.StatusCode == http.StatusNotFound:
		return false, nil
	case resp.StatusCode >= 200 && resp.StatusCode < 300:
		return true, nil
	default:
		return false, fmt.Errorf("check workflow %q: GitHub API returned %s", workflow, resp.Status)
	}
}

func escapePath(value string) string {
	parts := strings.Split(value, "/")
	for i, part := range parts {
		parts[i] = url.PathEscape(part)
	}
	return strings.Join(parts, "/")
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'"
}

func safeFilename(name string) bool {
	return safeName(name, false)
}

func safePattern(name string) bool {
	if !safeName(name, true) {
		return false
	}
	for _, placeholder := range []string{"{name}", "{version}", "{os}", "{arch}"} {
		name = strings.ReplaceAll(name, placeholder, "")
	}
	return !strings.ContainsAny(name, "{}")
}

func safeName(name string, patterns bool) bool {
	if name == "" {
		return false
	}
	for _, r := range name {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '.' || r == '_' || r == '-' || patterns && (r == '{' || r == '}')) {
			return false
		}
	}
	return true
}

//go:embed install.sh.tmpl
var installerTemplate string

//go:embed install.sh.header
var installerHeader string

const installerPreamble = `#!/bin/sh
# Code generated by insmith. DO NOT EDIT.
#
# The base and ideas of this installer come from godownloader.
# https://github.com/goreleaser/godownloader
set -eu

`

var scriptTemplate = template.Must(template.New("install.sh").Parse(
	installerPreamble + installerHeader + "\n" + installerTemplate,
))
