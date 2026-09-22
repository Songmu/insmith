package insmith

import (
	"context"
	"fmt"
	"io"
	"log"
)

const cmdName = "insmith"

// Run the insmith
func Run(ctx context.Context, argv []string, outStream, errStream io.Writer) error {
	log.SetOutput(errStream)
	return runGenerator(ctx, argv, outStream, errStream)
}

func printVersion(out io.Writer) error {
	_, err := fmt.Fprintf(out, "%s v%s (rev:%s)\n", cmdName, version, revision)
	return err
}
