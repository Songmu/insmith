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
	repository      string
	binary          string
	workflow        string
	assetPattern    string
	checksumPattern string
	verification    string
}

func runGenerator(args []string, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("insmith", flag.ContinueOnError)
	flags.SetOutput(stderr)
	flags.Usage = func() {
		fmt.Fprintln(stderr, "Usage: insmith [flags] OWNER/REPO")
		fmt.Fprintln(stderr)
		flags.PrintDefaults()
	}
	binary := flags.String("binary", "", "binary name (defaults to repository name)")
	workflow := flags.String("workflow", "release-build.yaml", "release workflow path for attestation verification")
	assetPattern := flags.String("asset-pattern", "", "asset name pattern using {binary}, {version}, {os}, and {arch}")
	checksumPattern := flags.String("checksum-pattern", "SHA256SUMS", "checksum asset name pattern")
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
		repository:      flags.Arg(0),
		binary:          *binary,
		workflow:        *workflow,
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
	parts := strings.Split(c.repository, "/")
	if len(parts) != 2 || !safeFilename(parts[0]) || !safeFilename(parts[1]) ||
		parts[0] == "." || parts[0] == ".." || parts[1] == "." || parts[1] == ".." {
		return "", fmt.Errorf("repository must be in OWNER/REPO form")
	}
	if c.binary == "" {
		c.binary = parts[1]
	}
	if !safeFilename(c.binary) || c.binary == "." || c.binary == ".." {
		return "", fmt.Errorf("binary must contain only letters, digits, dots, underscores, and hyphens")
	}
	if c.assetPattern == "" {
		c.assetPattern = "{binary}_{version}_{os}_{arch}"
	}
	if !safePattern(c.assetPattern) {
		return "", fmt.Errorf("asset pattern contains unsupported characters")
	}
	if c.checksumPattern != "" && !safePattern(c.checksumPattern) {
		return "", fmt.Errorf("checksum pattern contains unsupported characters")
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
	if c.checksumPattern == "" && c.verification != "none" && c.verification != "attestation" {
		return "", fmt.Errorf("checksum pattern must not be empty when checksum verification is enabled")
	}

	var output bytes.Buffer
	if err := scriptTemplate.Execute(&output, struct {
		Repository      string
		Binary          string
		Workflow        string
		AssetPattern    string
		ChecksumPattern string
		Verification    string
	}{
		shellQuote(c.repository),
		shellQuote(c.binary),
		shellQuote(c.workflow),
		shellQuote(c.assetPattern),
		shellQuote(c.checksumPattern),
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
	for _, placeholder := range []string{"{binary}", "{version}", "{os}", "{arch}"} {
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

var scriptTemplate = template.Must(template.New("install.sh").Parse(installerTemplate))
