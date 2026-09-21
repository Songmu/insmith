package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
)

func main() {
	if err := run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, "insmith:", err)
		os.Exit(1)
	}
}

func run(args []string, stdout, stderr io.Writer) error {
	if len(args) > 0 && args[0] == "generate" {
		args = args[1:]
	}

	flags := flag.NewFlagSet("insmith generate", flag.ContinueOnError)
	flags.SetOutput(stderr)
	binary := flags.String("binary", "", "binary name (defaults to repository name)")
	workflow := flags.String("workflow", "release-build.yaml", "release workflow path for attestation verification")
	assetPattern := flags.String("asset-pattern", "", "asset name pattern using {binary}, {version}, {os}, and {arch}")
	checksumPattern := flags.String("checksum-pattern", "SHA256SUMS", "checksum asset name pattern")
	verification := flags.String("verification", "attestation-or-checksum", "verification policy: attestation, attestation-or-checksum, checksum, or none")
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if flags.NArg() != 1 {
		flags.Usage()
		return fmt.Errorf("exactly one OWNER/REPO argument is required")
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
