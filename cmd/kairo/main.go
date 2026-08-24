// Command kairo is the Kairo Linux performance optimization CLI.
// Phase 3 adds transactional optimize/rollback with mandatory dry-run.
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/anjarman20/Kairo-Linux-Performance-Optimization-Toolkit/internal/backend"
	"github.com/anjarman20/Kairo-Linux-Performance-Optimization-Toolkit/internal/kairo"
	"github.com/anjarman20/Kairo-Linux-Performance-Optimization-Toolkit/internal/optimizer"
	"github.com/anjarman20/Kairo-Linux-Performance-Optimization-Toolkit/internal/profile"
	"github.com/anjarman20/Kairo-Linux-Performance-Optimization-Toolkit/internal/render"
)

const version = "0.3.0"

type opts struct {
	json    bool
	quiet   bool
	verbose bool
	config  string
	profile string
	dryRun  bool
	yesGo   bool
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

	ctx := context.Background()
	switch cmd {
	case "version":
		fmt.Printf("kairo %s\nkernel: %s %s\n", version, runtime.GOOS, runtime.GOARCH)
		return 0
	case "detect", "analyze":
		return runAnalyze(o, ctx)
	case "status":
		return runStatus(o, ctx)
	case "plan":
		return runPlan(o, ctx)
	case "optimize":
		return runOptimize(o, ctx)
	case "rollback":
		return runRollback(o, pos, ctx)
	case "profile":
		return runProfile(o, pos, ctx)
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n", cmd)
		usage()
		return 2
	}
}

// resolveProfile returns the requested profile, defaulting to balanced when
// none is given. Mutating commands always act on an explicit profile.
func resolveProfile(o opts) (profile.Profile, bool) {
	name := o.profile
	if name == "" {
		name = "balanced"
	}
	p, err := profile.Get(name)
	if err != nil {
		fmt.Fprintf(os.Stderr, "kairo: %v\n", err)
		return profile.Profile{}, false
	}
	return p, true
}

func runAnalyze(o opts, ctx context.Context) int {
	sc := kairo.Scan(ctx, version, o.profileProf(ctx))
	if o.json {
		if err := render.JSON(os.Stdout, sc); err != nil {
			fmt.Fprintln(os.Stderr, "kairo: json output failed: "+err.Error())
			return 1
		}
		return 0
	}
	render.Human(os.Stdout, sc, o.quiet, o.verbose)
	return 0
}

// profileProf resolves the optional --profile for read-only commands.
func (o opts) profileProf(ctx context.Context) *profile.Profile {
	if o.profile == "" {
		return nil
	}
	p, err := profile.Get(o.profile)
	if err != nil {
		fmt.Fprintln(os.Stderr, "kairo: "+err.Error())
		return nil
	}
	return &p
}

func runStatus(o opts, ctx context.Context) int {
	sc := kairo.Scan(ctx, version, o.profileProf(ctx))
	if o.json {
		var out struct {
			render.Scan
			LastTransaction string `json:"last_transaction,omitempty"`
		}
		out.Scan = sc
		out.LastTransaction = lastTxString()
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		enc.Encode(out)
		return 0
	}
	render.Human(os.Stdout, sc, o.quiet, o.verbose)
	render.HumanTransaction(os.Stdout, lastTxString())
	return 0
}

// lastTxString summarizes the latest transaction, or a "none yet" line.
func lastTxString() string {
	id, err := optimizer.Latest(optimizer.DefaultBase)
	if err != nil {
		return ""
	}
	if id == "" {
		return "Optimization transactions: none yet.\n"
	}
	m, err := optimizer.LoadSnapshot(optimizer.DefaultBase, id)
	if err != nil {
		return ""
	}
	return fmt.Sprintf("Last optimization: %s (profile %s, state %s)\n", id, m.Profile, m.State)
}

func runPlan(o opts, ctx context.Context) int {
	p, ok := resolveProfile(o)
	if !ok {
		return 1
	}
	sc := kairo.Scan(ctx, version, &p)
	plan := optimizer.Build(p, sc)
	if o.json {
		data, _ := json.MarshalIndent(plan, "", "  ")
		fmt.Println(string(data))
		return 0
	}
	optimizer.PlanHuman(os.Stdout, plan, o.quiet)
	return 0
}

func runOptimize(o opts, ctx context.Context) int {
	p, ok := resolveProfile(o)
	if !ok {
		return 1
	}
	sc := kairo.Scan(ctx, version, &p)
	plan := optimizer.Build(p, sc)

	optimizer.PlanHuman(os.Stdout, plan, o.quiet)
	if len(plan.Changes) == 0 {
		fmt.Println("No changes needed.")
		return 0
	}
	if o.dryRun {
		fmt.Println("\nNo changes have been applied.")
		return 0
	}
	if !o.yesGo && !confirmApply() {
		fmt.Println("Aborted. Run with --dry-run to preview without applying.")
		return 1
	}
	if os.Geteuid() != 0 {
		fmt.Fprintln(os.Stderr, "This operation requires root privileges.\n\nRun:\n  sudo kairo optimize")
		return 1
	}

	meta := optimizer.Metadata{
		ID:           optimizer.NewID(time.Now()),
		Timestamp:    time.Now().UTC().Format(time.RFC3339),
		KairoVersion: version,
		Kernel:       sc.System.Kernel,
		Hostname:     sc.System.Hostname,
		Profile:      p.Name,
		State:        optimizer.StateSnapshot,
		Changes:      plan.Changes,
	}
	if err := optimizer.SaveSnapshot(optimizer.DefaultBase, meta); err != nil {
		fmt.Fprintf(os.Stderr, "kairo: failed to save transaction snapshot: %v\n", err)
		return 1
	}
	if err := optimizer.Apply(ctx, backend.Real{}, plan.Changes); err != nil {
		optimizer.SetState(optimizer.DefaultBase, meta.ID, optimizer.StateFailedRoll)
		fmt.Fprintf(os.Stderr, "\nOptimization failed.\nThe system was restored to the previous state.\n\nTransaction: %s\n%v\n", meta.ID, err)
		return 1
	}
	if err := optimizer.CommitTransaction(optimizer.DefaultBase, meta.ID); err != nil {
		fmt.Fprintf(os.Stderr, "kairo: changes applied but failed to commit metadata: %v\n", err)
		return 1
	}

	fmt.Printf("\n%d change(s) applied and verified.\nTransaction: %s\n\nRuntime change applied.\nThis change will not survive reboot.\n", len(plan.Changes), meta.ID)
	return 0
}

func runRollback(o opts, pos []string, ctx context.Context) int {
	if os.Geteuid() != 0 {
		fmt.Fprintln(os.Stderr, "This operation requires root privileges.\n\nRun:\n  sudo kairo rollback")
		return 1
	}
	id := ""
	if len(pos) > 0 {
		id = pos[0]
	} else {
		id, _ = optimizer.Latest(optimizer.DefaultBase)
	}
	if id == "" {
		fmt.Println("No optimization transactions found.")
		return 0
	}
	m, err := optimizer.LoadSnapshot(optimizer.DefaultBase, id)
	if err != nil {
		fmt.Fprintf(os.Stderr, "kairo: %v\n", err)
		return 1
	}
	if m.State == optimizer.StateRolledBack {
		fmt.Printf("Transaction %s is already rolled back.\n", id)
		return 0
	}
	if err := optimizer.Rollback(ctx, backend.Real{}, m.Changes); err != nil {
		fmt.Fprintf(os.Stderr, "Rollback failed: %v\n", err)
		return 1
	}
	if err := optimizer.SetState(optimizer.DefaultBase, id, optimizer.StateRolledBack); err != nil {
		fmt.Fprintf(os.Stderr, "kairo: values restored but metadata update failed: %v\n", err)
		return 1
	}
	fmt.Printf("Rollback complete.\n\nTransaction: %s\nPrevious values restored and verified.\n", id)
	return 0
}

// runProfile handles the profile subcommand tree; apply routes into the same
// transactional optimize pipeline as kairo optimize --profile=<name>.
func runProfile(o opts, args []string, ctx context.Context) int {
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
		if len(args) < 2 {
			fmt.Fprintln(os.Stderr, "kairo: profile apply requires a name")
			return 2
		}
		o.profile = args[1]
		return runOptimize(o, ctx)
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

// confirmApply reads a y/N answer from the terminal, defaulting to No so an
// accidental Enter never mutates the system.
func confirmApply() bool {
	fmt.Print("\nApply these changes? [y/N] ")
	r := bufio.NewReader(os.Stdin)
	line, _ := r.ReadString('\n')
	return strings.TrimSpace(strings.ToLower(line)) == "y"
}

// parseArgs collects global flags (any order), the first non-flag token as
// the subcommand, and remaining positional args for subcommands.
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
		case "--dry-run":
			o.dryRun = true
		case "--yes":
			o.yesGo = true
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
	fmt.Print(`kairo - Linux performance optimization toolkit

Usage:
  kairo <command> [flags]

Commands:
  detect               probe raw system capabilities
  analyze              structured analysis with transparent score
  status               current state summary + last transaction
  plan                 show what a profile would change (never applies)
  optimize             apply a profile transactionally (dry-run first)
  rollback [<id>]      restore previous values of one transaction
  profile              list / show / apply workload profiles
  version              version and kernel info

Flags:
  --json         machine-readable output only
  --quiet, -q    summary lines only
  --verbose, -v  extra context
  --config=PATH  configuration file (reserved)
  --profile=NAME select workload profile
  --dry-run      preview changes; nothing is written
  --yes          skip the apply confirmation prompt
`)
}
