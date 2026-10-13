package main

import (
	"fmt"
	"os"

	"doggo/internal/toolchain/codegen"
)

func main() {
	if err := codegen.RunInteropGenCLI(os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "interop generator failed: %v\n", err)
		os.Exit(1)
	}
}
