// Command license-notices generates deterministic CLI redistribution notices and
// verifies the license documents in CLI release archives and VS Code VSIX files.
package main

import (
	"flag"
	"fmt"
	"os"

	"doggo/internal/licenses"
)

type paths []string

func (p *paths) String() string         { return fmt.Sprint([]string(*p)) }
func (p *paths) Set(value string) error { *p = append(*p, value); return nil }

func main() {
	root := flag.String("root", ".", "repository root")
	check := flag.Bool("check", false, "check generated Go notices without changing files")
	var artifacts paths
	flag.Var(&artifacts, "verify-artifact", "verify an actual CLI .zip/.tar.gz or extension .vsix (repeatable)")
	flag.Parse()
	if flag.NArg() != 0 {
		fmt.Fprintln(os.Stderr, "unexpected positional arguments")
		os.Exit(2)
	}
	if len(artifacts) == 0 || *check {
		if err := licenses.WriteGo(*root, *check); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	}
	for _, path := range artifacts {
		if err := licenses.VerifyArtifact(*root, path); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		fmt.Printf("Verified license documents: %s\n", path)
	}
}
