# Kairo

Linux performance analysis and optimization toolkit.

Detect what your server is doing, select a workload profile, preview every
change, then apply only the smallest set of justified, reversible
optimizations — and prove the result with benchmarks.

## Install

```bash
make build        # bin/kairo
```

## Usage

```bash
kairo analyze                  # full system report + transparent score
kairo analyze --json           # machine-readable output for automation
kairo analyze --profile gaming # compare a workload profile vs live state
kairo detect                   # raw capability probe
kairo profile list             # available workload profiles
kairo profile show gaming      # what a profile targets
kairo status
kairo version

# Planned (safe optimization phases)
kairo optimize --dry-run
kairo profile apply gaming --dry-run
kairo rollback <transaction-id>
kairo benchmark storage --save before.json
```

## Global flags

`--json`, `--quiet`, `--verbose`, `--config=PATH`, `--dry-run`, `--yes`.

## Design principles

- Detection before modification: nothing changes until you see the plan.
- Every mutation reversible, with a stored snapshot and rollback.
- Native Linux interfaces (`/proc`, `/sys`, sysctl) preferred over parsing
  human-oriented command output.
- No assumptions about CPU vendor, NIC, disk, or kernel version.
- Runs fully offline. No telemetry, no accounts, no cloud dependencies.

## License

Apache License 2.0. See [LICENSE](LICENSE).