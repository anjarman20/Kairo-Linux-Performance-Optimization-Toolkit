// Command kairo is the Kairo Linux performance analysis CLI (Phase 1:
// detection only). All commands are read-only; optimization lands Phase 3.
package main

import (
	"context"
	"fmt"
	"os"
	"runtime"
	"sort"
	"strings"

	"github.com/anjarman20/Kairo-Linux-Performance-Optimization-Toolkit/internal/kairo"
	"github.com/anjarman20/Kairo-Linux-Performance-Optimization-Toolkit/internal/profile"
	"github.com/anjarman20/Kairo-Linux-Performance-Optimization-Toolkit/internal/render"
)

const version = "0.2.0"

type opts struct {
	json    bool
	quiet   bool
	verbose bool
	config  string
	profile string
}

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	o, cmd, pos, errs := parseArgs(args)
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
		var prof *profile.Profile
		if o.profile != "" {
			p, err := profile.Get(o.profile)
			if err != nil {
				fmt.Fprintln(os.Stderr, "kairo: "+err.Error())
				return 1
			}
			prof = &p
		}
		sc := kairo.Scan(context.Background(), version, prof)
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
	case "profile":
		return runProfile(o, pos)
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n", cmd)
		usage()
		return 2
	}
}

// runProfile handles the profile subcommand tree. apply lands in Phase 3 and
// fails cleanly rather than pretending to be ready.
func runProfile(o opts, args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "kairo: profile requires list, show, or apply")
		return 2
	}
	switch args[0] {
	case "list":
		names, err := profile.Names()
		if err != nil {
			fmt.Fprintln(os.Stderr, "kairo: "+err.Error())
			return 1
		}
		fmt.Println("Available profiles")
		for _, n := range names {
			p, _ := profile.Get(n)
			fmt.Printf("  %-14s %s\n", p.Name, p.Description)
		}
		return 0
	case "show":
		if len(args) < 2 {
			fmt.Fprintln(os.Stderr, "kairo: profile show requires a name")
			return 2
		}
		p, err := profile.Get(args[1])
		if err != nil {
			fmt.Fprintln(os.Stderr, "kairo: "+err.Error())
			return 1
		}
		fmt.Printf("Profile: %s\n  %s\n\nTargets\n", p.Name, p.Description)
		areas := make([]string, 0, len(p.Targets))
		for area := range p.Targets {
			areas = append(areas, area)
		}
		sort.Strings(areas)
		for _, area := range areas {
			for _, k := range sortedKeys(p.Targets[area]) {
				fmt.Printf("  %s.%s: %s\n", area, k, p.Targets[area][k])
			}
		}
		return 0
	case "apply":
		fmt.Fprintln(os.Stderr, "kairo: profile apply arrives in Phase 3 (plan/optimize/rollback).")
		return 2
	default:
		fmt.Fprintf(os.Stderr, "kairo: unknown profile command %q\n", args[0])
		return 2
	}
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// parseArgs collects global flags (any order), the first non-flag token as
// the subcommand, remaining positional args for subcommands, and flag errors.
// Mutating flags --dry-run/--yes are accepted now and become meaningful in
// Phase 3.
func parseArgs(args []string) (opts, string, []string, []string) {
	var o opts
	var cmd string
	var pos []string
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
		case "--profile=":
			errs = append(errs, "--profile requires a name")
		case "--version":
			cmd = "version"
		default:
			switch {
			case strings.HasPrefix(a, "--config="):
				o.config = strings.TrimPrefix(a, "--config=")
			case strings.HasPrefix(a, "--profile="):
				o.profile = strings.TrimPrefix(a, "--profile=")
			case strings.HasPrefix(a, "-c="):
				o.config = strings.TrimPrefix(a, "-c=")
			case strings.HasPrefix(a, "-"):
				errs = append(errs, "unknown flag: "+a)
			case cmd == "":
				cmd = a
			default:
				pos = append(pos, a)
			}
		}
	}
	return o, cmd, pos, errs
}

func usage() {
	fmt.Print(`kairo - Linux performance analysis toolkit

Usage:
  kairo <command> [flags]

Commands:
  detect     probe raw system capabilities
  analyze    structured analysis with transparent score
  status     current state summary
  profile    list / show / apply workload profiles
  version    version and kernel info

Flags:
  --json         machine-readable output only
  --quiet, -q    summary lines only
  --verbose, -v  extra context
  --config=PATH  configuration file (reserved)
  --profile=NAME compare targets against live state (analyze only)
  --dry-run      preview mode (Phase 3)
  --yes          skip prompts (Phase 3)
`)
}
