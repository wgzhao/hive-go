# hive-go

Pure-Go HiveServer2 client and CLI. No JVM, Hadoop client libraries, CGO, or
Hive installation are required. The project/module is `hive-go`; the installed
command is `hive`.

## Layout

- `hive.go` — public types: `Config`, `AuthMode`, `Column`, `Value`, `Row`.
- `client.go` — `Connect`, session lifecycle, protocol selection, status/error
  conversion.
- `execute.go` — `Execute`, operation polling, metadata loading, cancellation.
- `result.go` — `Result`, lazy `FetchResults` paging, `Close`.
- `decode.go` — columnar, row-based, and blob rowset decoding; type metadata.
- `errors.go` — `ServerError`, `ErrCanceled`, `ErrClosed`.
- `internal/transport/` — TCP dial, SASL PLAIN handshake, framed/buffered
  Thrift transports.
- `internal/tcliservice/` — Hive 4.0.0 `TCLIService.thrift` and generated Go
  code (committed; do not edit by hand).
- `internal/sqlsplit/` — quote/comment-aware SQL statement splitter and
  completeness check.
- `internal/hiveconf/` — `hive-site.xml` reader.
- `cmd/hive/` — CLI: flags/config, batch stream, REPL, meta commands, output
  formats.
- `cmd/hive/e2e/` — real-server E2E suite, goldens, artifacts.

## Protocol contract

HiveServer2 uses Thrift `TCLIService` over a binary protocol.

1. TCP connect.
2. `NONE`/`LDAP`: SASL PLAIN handshake. Thrift SASL messages are one status
   byte plus a 4-byte big-endian length and payload. The Java server reads
   `START`, creates the SASL server, and immediately waits for the client's
   initial response; send `START "PLAIN"` and `OK "\x00user\x00password"`
   back-to-back. After `COMPLETE`, wrap the raw connection in
   `thrift.TFramedTransport`.
3. `NOSASL`: `thrift.TBufferedTransport`, no SASL.
4. `OpenSession` with protocol V10 (V6 via `--protocol v6`).
5. `ExecuteStatement(runAsync=true)`, poll `GetOperationStatus` with backoff.
6. If `hasResultSet`, `GetResultSetMetadata`, then `FetchResults(FETCH_NEXT)`
   until an empty batch.
7. Always `CloseOperation`, `CloseSession`, and close the transport.

Important server quirks:

- `hasMoreRows=false` can accompany a non-empty batch while more rows exist.
  Terminate on an empty batch, not on `hasMoreRows`.
- Result batches can be `binaryColumns` (blob), `columns` (V6+ columnar), or
  legacy `rows`. Blob data is a sequence of `TColumn` structs serialized with
  `TCompactProtocol`, count from `columnCount`.
- NULL bitmaps: bit set means NULL, LSB-first within each byte:
  `nulls[i/8] & (1 << (i%8)) != 0`. The bitmap may have a trailing byte.
- TIMESTAMP, DATE, DECIMAL, CHAR/VARCHAR, INTERVAL, ARRAY, MAP, and STRUCT are
  returned as strings in practice. Print the server text; do not re-parse it.
- Hive 3 can report `hasMoreRows` incorrectly; see above.

## Concurrency and cancellation

The Thrift client/transport is not safe for concurrent calls. Execute, Next, and
Close are serialized on the calling goroutine. Cancellation uses the context:
the polling loop calls `CancelOperation` from the same goroutine when the
context is canceled. `Result.Next` checks the context before each batch.
After cancellation the session is reusable.

## Testing policy

- Do not add unit tests. The project verifies behavior with E2E tests against a
  real HiveServer2 (`cmd/hive/e2e/run.sh`), and produces an artifact
  directory (`env.txt`, per-case stdout/stderr/exit, `report.txt`,
  `report.json`, cleanup result).
- E2E must use a uniquely named temporary database and drop it with
  `DROP DATABASE ... CASCADE`. In the verified environment, use
  `HIVE_USER=hive`; the default OS user cannot write the managed warehouse.
- The verified environment was Hive `3.1.0.3.1.4.0-315`, auth `NONE`, protocol
  V10. Examples default to `127.0.0.1:10000`; set `HIVE_HOST`/`HIVE_PORT` for
  your endpoint. `HIVE_ALLOW_DDL=1` is required.
- GitHub-hosted CI cannot reach the verified private endpoint, so the `Tests`
  workflow starts the official `apache/hive:4.0.1` image locally
  (`.github/scripts/install-hive.sh`, embedded Derby, `NONE` auth) and runs the
  same E2E suite against it. It forces `hive.execution.engine=mr`: the image's
  Tez-on-local-filesystem path drops blob-rowset staging files, which makes the
  blob case return an empty result. Per-case artifacts are uploaded by the
  workflow. Runs against other endpoints are manual or on a self-hosted runner.
- `make e2e` requires the environment shown in the README. `E2E_SLOW=1` adds
  cancellation; `UPDATE_GOLDEN=1` rewrites goldens.

## Code generation

Generated files are committed; normal builds need no Thrift compiler.

```bash
thrift --version   # must be 0.19.0
make generate
```

The IDL is `internal/tcliservice/TCLIService.thrift` from Apache Hive 4.0.0.
The Go package is flattened to `internal/tcliservice` after generation. Keep the
Thrift runtime pinned to `v0.19.0` and use `GOTOOLCHAIN=local` to avoid an
accidental Go version bump.

## CLI conventions

- stdout is result data only. Status, info, warnings, errors, timing, and
  connection messages go to stderr.
- Exit codes: 0 success, 1 SQL/connection/I/O error, 2 usage/configuration
  error, 130 interrupted.
- `-e` and `-f` are mutually exclusive. `-f -` reads stdin.
- Non-TTY stdin is treated as a script; it supports SQL and `!` meta commands.
- `--output-format`: table, vertical, csv, tsv, json, jsonl. `--null-string`,
  `--null-empty` and `--no-header` apply to the tabular formats and CSV/TSV.
  `--max-column-width` applies only to table/vertical; `--binary-format`
  controls how BINARY values are rendered in every format.
- `--hiveconf K=V` sets session configuration; `--hivevar K=V` defines client
  variables used by `${hivevar:...}`, `${hiveconf:...}`, `${env:...}` and
  `${system:...}` substitution. Undefined variables are an error.
- The REPL uses `github.com/chzyer/readline`; batch/stream mode uses a plain
  line scanner.

## Commits and PRs

Do not include AI-agent attribution, agent names, email addresses, `Co-Authored-By`
lines, or "Generated with ..." footers in commit messages or PR descriptions.
