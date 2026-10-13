package main

import (
	"fmt"
	"os"

	"doggo/internal/toolchain/codegen"
)

func main() {
	if err := codegen.RunReifyGenCLI(os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "reify_gen failed: %v\n", err)
		os.Exit(1)
	}
}
