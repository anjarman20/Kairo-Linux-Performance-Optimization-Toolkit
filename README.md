# Kairo

Open-source Linux performance analysis and optimization toolkit.

Phase 1 status: read-only detection CLI. Detect → Analyze → Plan → Show →
Apply → Verify → Benchmark pipeline in later phases.

## Build

```bash
make build        # bin/kairo
make test         # unit tests, no root, no network
make vet
```

## Usage

```bash
kairo analyze              # human report + transparent score
kairo analyze --json       # stable machine-readable output only
kairo detect               # raw capability probe
kairo status
kairo version
```

Global flags: `--json`, `--quiet`, `--verbose`, `--config=PATH`,
`--dry-run`, `--yes` (mutating flags active in Phase 3).

## Principles

- Safety first: every change reversible, dry-run mandatory before mutating.
- Native interfaces (`/proc`, `/sys`, sysctl) over shell parsing.
- No assumptions about CPU vendor, NIC, kernel, or NUMA layout.
- **zswap/zram are out of scope** — never modified, never recommended.
- No firewall/SSH management.
- Fully offline, no telemetry, no cloud dependencies.

## Docs

- [PRD](docs/PRD.md)
- [Roadmap](docs/ROADMAP.md)
- [AGENTS.md](AGENTS.md)

## License

TBD by project owner.