// Command kairo is the Kairo Linux performance analysis CLI (Phase 1:
// detection only). All commands are read-only; optimization lands Phase 3.
package main

import (
	"context"
	"fmt"
	"os"
	"runtime"
	"strings"

	"github.com/anjarman20/Kairo-Linux-Performance-Optimization-Toolkit/internal/kairo"
	"github.com/anjarman20/Kairo-Linux-Performance-Optimization-Toolkit/internal/render"
)

const version = "0.1.0"

type opts struct {
	json    bool
	quiet   bool
	verbose bool
	config  string
}

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	o, cmd, errs := parseArgs(args)
	for _, e := range errs {
		fmt.Fprintln(os.Stderr, "kairo: "+e)
	}
	if cmd == "" || cmd == "help" {
		usage()
		return 0
	}

	switch cmd {
	case "version":
		fmt.Printf("kairo %s\nkernel: %s %s\n", version, runtime.GOOS, runtime.GOARCH)
		return 0
	case "detect", "analyze", "status":
		sc := kairo.Scan(context.Background(), version)
		if o.json {
			if err := render.JSON(os.Stdout, sc); err != nil {
				fmt.Fprintln(os.Stderr, "kairo: json output failed: "+err.Error())
				return 1
			}
			return 0
		}
		if cmd == "status" {
			fmt.Println("Kairo status: no transaction history yet (Phase 3).")
		}
		render.Human(os.Stdout, sc, o.quiet, o.verbose)
		return 0
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n", cmd)
		usage()
		return 2
	}
}

// parseArgs collects global flags (any order) and the first non-flag token as
// the subcommand. Mutating flags --dry-run/--yes are accepted now and become
// meaningful in Phase 3.
func parseArgs(args []string) (opts, string, []string) {
	var o opts
	var cmd string
	var errs []string
	for _, a := range args {
		switch a {
		case "--json", "-json":
			o.json = true
		case "--quiet", "-q":
			o.quiet = true
		case "--verbose", "-v":
			o.verbose = true
		case "--dry-run", "--yes": // reserved for Phase 3
		case "--help", "-h":
			cmd = "help"
		case "--config=":
			errs = append(errs, "--config requires a path")
		case "--version":
			cmd = "version"
		default:
			switch {
			case strings.HasPrefix(a, "--config="):
				o.config = strings.TrimPrefix(a, "--config=")
			case strings.HasPrefix(a, "-c="):
				o.config = strings.TrimPrefix(a, "-c=")
			case strings.HasPrefix(a, "-"):
				errs = append(errs, "unknown flag: "+a)
			case cmd == "":
				cmd = a
			default:
				errs = append(errs, "unexpected argument: "+a)
			}
		}
	}
	return o, cmd, errs
}

func usage() {
	fmt.Print(`kairo - Linux performance analysis toolkit (Phase 1: detection)

Usage:
  kairo <command> [flags]

Commands:
  detect     probe raw system capabilities
  analyze    structured analysis with transparent score
  status     current state summary
  version    version and kernel info

Flags:
  --json         machine-readable output only
  --quiet, -q    summary lines only
  --verbose, -v  extra context
  --config=PATH  configuration file (reserved)
  --dry-run      preview mode (Phase 3)
  --yes          skip prompts (Phase 3)
`)
}
