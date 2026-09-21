package insmith

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log"
	"strings"
)

const cmdName = "insmith"

// Run the insmith
func Run(ctx context.Context, argv []string, outStream, errStream io.Writer) error {
	log.SetOutput(errStream)
	if len(argv) > 0 {
		if argv[0] == "generate" {
			return runGenerate(argv[1:], outStream, errStream)
		}
		if !strings.HasPrefix(argv[0], "-") || !isGlobalFlag(argv[0]) {
			return runGenerate(argv, outStream, errStream)
		}
	}
	fs := flag.NewFlagSet(
		fmt.Sprintf("%s (v%s rev:%s)", cmdName, version, revision), flag.ContinueOnError)
	fs.SetOutput(errStream)
	ver := fs.Bool("version", false, "display version")
	if err := fs.Parse(argv); err != nil {
		return err
	}
	if *ver {
		return printVersion(outStream)
	}
	return nil
}

func isGlobalFlag(arg string) bool {
	switch strings.TrimLeft(arg, "-") {
	case "h", "help", "version":
		return true
	default:
		return false
	}
}

func printVersion(out io.Writer) error {
	_, err := fmt.Fprintf(out, "%s v%s (rev:%s)\n", cmdName, version, revision)
	return err
}
