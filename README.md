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

# Prescriptive optimization (transactional, reversible)
kairo plan --profile database              # preview only, writes nothing
kairo optimize --profile database --dry-run
kairo optimize --profile database          # prompts [y/N] before applying
kairo rollback <transaction-id>            # restore previous values
kairo rollback                             # roll back the latest transaction

# Benchmarking (before/after)
kairo benchmark storage --save before.json
kairo optimize --profile database --yes
kairo benchmark storage --save after.json
kairo benchmark compare before.json after.json
kairo benchmark all
kairo benchmark system
```

## Global flags

`--json`, `--quiet`, `--verbose`, `--config=PATH`, `--profile=NAME`,
`--dry-run`, `--yes`, `--save=PATH`.

## Design principles

- Detection before modification: nothing changes until you see the plan.
- Every mutation reversible, with a stored snapshot and rollback.
- Native Linux interfaces (`/proc`, `/sys`, sysctl) preferred over parsing
  human-oriented command output.
- No assumptions about CPU vendor, NIC, disk, or kernel version.
- Runs fully offline. No telemetry, no accounts, no cloud dependencies.

## License

Apache License 2.0. See [LICENSE](LICENSE).