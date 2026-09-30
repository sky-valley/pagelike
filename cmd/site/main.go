// Command site builds and serves the pagelike docs site.
//
// The site is generated from the existing repo docs (README.md, AGENTS.md,
// docs/spec/, docs/decisions/, docs/compat/, etc.) plus site/content/ for
// additions aimed at agents. Output is plain static HTML under site/public/,
// fit for GitHub Pages or any other static host.
//
// This is a build-time binary. The pagelike runtime (internal/dom) is not
// used here; markdown -> HTML goes through Goldmark and the layout uses the
// Go standard library html/template. Per internal/dom/AGENTS.md the runtime
// constraint about html.Parse/Fragment/Render applies to runtime code, not
// build artefacts.
package main

import (
	"flag"
	"fmt"
	"os"
)

const usage = `site builds the pagelike docs site.

Usage:
  site build  [--src DIR] [--base-url URL] [--out DIR]   Re-emit the site
  site serve  [--out DIR] [--addr 127.0.0.1:9000]         Preview the build

Defaults match the repo layout:
  --src       . (current directory, assumed to be the repo root)
  --out       site/public
  --base-url  https://sky-valley.github.io/pagelike
  --addr      127.0.0.1:9000
`

func main() {
	if len(os.Args) < 2 {
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}
	var err error
	switch os.Args[1] {
	case "build":
		err = cmdBuild(os.Args[2:])
	case "serve":
		err = cmdServe(os.Args[2:])
	case "-h", "--help", "help":
		fmt.Print(usage)
		return
	default:
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "site:", err)
		os.Exit(1)
	}
}

// flagSet is the helper used by every subcommand so error reporting stays
// uniform.  ContinueOnError lets us return errors instead of exiting.
func flagSet(name string) *flag.FlagSet {
	return flag.NewFlagSet(name, flag.ContinueOnError)
}
