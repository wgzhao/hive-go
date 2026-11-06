# Contributing to hive-go

Thanks for helping. `hive-go` is a small, focused HiveServer2 client; the best
contributions keep it small, correct, and easy to operate.

By participating, you agree to follow the [Code of Conduct](CODE_OF_CONDUCT.md).

## Before you start

- Open an issue for anything larger than a typo or an obvious bug fix. CLI flags,
  public API shape, protocol behavior, and new runtime dependencies deserve a
  short discussion first.
- Report security problems privately as described in [SECURITY.md](SECURITY.md),
  not in a public issue.
- Contributions are accepted under the project's MIT license. The generated
  `TCLIService` code remains Apache-2.0; see `NOTICE`.

## Scope and design constraints

- **Pure Go, no CGO.** The binary must keep building with `CGO_ENABLED=0` on
  Linux, macOS, and Windows.
- **No JVM, Hadoop client libraries, or Hive installation.** If a feature needs
  one of those, it belongs in a different project.
- **Talk to the server, don't guess.** Protocol and type behavior should follow
  what HiveServer2 actually returns. The quirks documented in the README are
  deliberate compatibility decisions.
- **Keep the binary self-contained.** New runtime dependencies need a concrete
  justification (size, maintenance, portability) in the pull request.
- **Respect the CLI contract.** Result data goes to stdout; status, warnings,
  errors, and timing go to stderr. Exit codes are 0 success, 1 SQL/connection/I/O
  error, 2 usage/configuration error, and 130 interrupted.

## Development setup

Requirements:

- Go 1.22 or newer.
- Apache Thrift 0.19.0 *only* if you need to regenerate the protocol code.

```bash
make build     # ./hive
make test      # go vet ./... (there are no unit tests)
make e2e       # requires a real HiveServer2, see below
```

Generated files under `internal/tcliservice/` are committed. Do not edit them by
hand; change `TCLIService.thrift` and run `make generate` instead. Keep the
Thrift Go runtime pinned to `v0.19.0` and use `GOTOOLCHAIN=local` so a build does
not silently upgrade Go.

## Testing policy

This project deliberately has **no unit tests**. Behavior is verified by the E2E
suite in `cmd/hive/e2e/run.sh` against a real HiveServer2:

1. If you are adding a feature or fixing a bug, first list the ways it can fail
   (wrong null bitmap, empty final batch, non-ASCII data, cancellation midway,
   protocol fallback, and so on).
2. Add or extend an E2E case that exercises a realistic, medium-to-hard
   scenario, not just the happy path.
3. Run the suite and include the result in the pull request.

```bash
HIVE_HOST=127.0.0.1 \
HIVE_PORT=10000 \
HIVE_AUTH=none \
HIVE_USER=hive \
HIVE_ALLOW_DDL=1 \
make e2e
```

- Use a temporary database. The suite creates and drops one with
  `DROP DATABASE ... CASCADE`; do not point it at production.
- `E2E_SLOW=1` adds the cancellation case.
- `UPDATE_GOLDEN=1` rewrites `cmd/hive/e2e/golden/*.txt`. Only use it when the
  output change is intentional, and review the diff.
- Artifacts land in `cmd/hive/e2e/artifacts/<timestamp>_<pid>/` and include
  `report.txt`, `report.json`, per-case stdout/stderr/exit, and cleanup status.
  Attach or quote the relevant artifact in the pull request.

The GitHub-hosted suite starts `apache/hive:4.0.1` locally; the private verified
endpoint is not reachable from hosted runners. If you cannot run the suite,
say so explicitly in the pull request rather than claiming it passed.

## Code style

- Run `gofmt` and `go vet` before sending a change.
- Prefer standard-library solutions and small, direct functions.
- Keep errors actionable: include the server message where it helps, but never
  print passwords or full connection URLs containing credentials.
- Preserve session reuse after cancellation and serialize access to the Thrift
  transport; it is not safe for concurrent calls.
- Update `README.md`, `--help` output, and the E2E goldens when a change is
  user-visible.

## Pull requests

A good pull request:

- Contains one logical change.
- Explains the user-visible behavior and the reason for it.
- States how it was verified (E2E artifact path, server version, auth mode,
  protocol), or why E2E could not be run.
- Includes documentation updates where behavior changed.
- Does not commit `cmd/hive/e2e/artifacts/`, local binaries, or editor files.

Maintainers may ask for a narrower change or an additional E2E case. For
security reports, follow [SECURITY.md](SECURITY.md) instead of opening a pull
request.

## Commit messages

Write short, imperative messages focused on the change, for example:

```
cli: stream JSON output in fetch-size batches
protocol: terminate on an empty batch, not hasMoreRows
```

Avoid mixing refactors, formatting, and behavior changes in one commit.
