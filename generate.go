package insmith

import (
	"bytes"
	_ "embed"
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"
	"text/template"
)

type config struct {
	repository   string
	name         string
	binaries     []string
	workflow     string
	assetPattern string
	checksumFile string
	verification string
}

type stringListFlag []string

func (f *stringListFlag) String() string {
	return strings.Join(*f, ",")
}

func (f *stringListFlag) Set(value string) error {
	*f = append(*f, value)
	return nil
}

func runGenerator(args []string, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("insmith", flag.ContinueOnError)
	flags.SetOutput(stderr)
	flags.Usage = func() {
		fmt.Fprintln(stderr, "Usage: insmith [flags] OWNER/REPO")
		fmt.Fprintln(stderr)
		flags.PrintDefaults()
	}
	name := flags.String("name", "", "release package name (defaults to repository name)")
	var binaries stringListFlag
	flags.Var(&binaries, "binary", "binary to install (repeatable; defaults to name)")
	workflow := flags.String("workflow", "release-build.yaml", "release workflow path for attestation verification (empty disables workflow pinning)")
	assetPattern := flags.String("asset-pattern", "", "asset name pattern using {name}, {version}, {os}, and {arch}")
	checksumFile := flags.String("checksum-file", "SHA256SUMS", "checksum file name, which may use {name}, {version}, {os}, and {arch}")
	verification := flags.String("verification", "attestation-or-checksum", "verification policy: attestation, attestation-or-checksum, checksum, or none")
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
	if flags.NArg() != 1 {
		flags.Usage()
		return errors.New("exactly one OWNER/REPO argument is required; flags must precede it")
	}

	script, err := generate(config{
		repository:   flags.Arg(0),
		name:         *name,
		binaries:     binaries,
		workflow:     *workflow,
		assetPattern: *assetPattern,
		checksumFile: *checksumFile,
		verification: *verification,
	})
	if err != nil {
		return err
	}
	_, err = io.WriteString(stdout, script)
	return err
}

func generate(c config) (string, error) {
	parts := strings.Split(c.repository, "/")
	if len(parts) != 2 || !safeFilename(parts[0]) || !safeFilename(parts[1]) ||
		parts[0] == "." || parts[0] == ".." || parts[1] == "." || parts[1] == ".." {
		return "", fmt.Errorf("repository must be in OWNER/REPO form")
	}
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
	if c.checksumFile != "" && !safePattern(c.checksumFile) {
		return "", fmt.Errorf("checksum file name contains unsupported characters")
	}
	if c.workflow != "" && !strings.Contains(c.workflow, "/.github/workflows/") {
		c.workflow = strings.TrimPrefix(c.workflow, "/")
		if !strings.HasPrefix(c.workflow, ".github/workflows/") {
			c.workflow = ".github/workflows/" + c.workflow
		}
		c.workflow = c.repository + "/" + c.workflow
	}
	switch c.verification {
	case "attestation", "attestation-or-checksum", "checksum", "none":
	default:
		return "", fmt.Errorf("unknown verification policy %q", c.verification)
	}
	if c.checksumFile == "" && c.verification != "none" && c.verification != "attestation" {
		return "", fmt.Errorf("checksum file name must not be empty when checksum verification is enabled")
	}

	var output bytes.Buffer
	if err := scriptTemplate.Execute(&output, struct {
		Repository   string
		Name         string
		Binaries     string
		Workflow     string
		AssetPattern string
		ChecksumFile string
		Verification string
	}{
		shellQuote(c.repository),
		shellQuote(c.name),
		shellQuote(strings.Join(c.binaries, " ")),
		shellQuote(c.workflow),
		shellQuote(c.assetPattern),
		shellQuote(c.checksumFile),
		shellQuote(c.verification),
	}); err != nil {
		return "", err
	}
	return output.String(), nil
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
