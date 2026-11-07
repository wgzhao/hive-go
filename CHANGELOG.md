# Changelog

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- `--hiveconf K=V` session properties and `--hivevar K=V` client variables,
  with `${hivevar:...}`, `${hiveconf:...}`, `${env:...}` and `${system:...}`
  substitution in scripts and meta commands. Undefined variables are an error;
  `\${...}` escapes a reference.
- `-i/--init-file` for a startup script, `--force` to continue after statement
  errors, and `--delimiter` for multi-character statement terminators.
- `-w/--password-file`, with a warning when the file is accessible by other
  users.
- `;hiveconf:` and `;hivevar:` JDBC URL parameters. Unsupported options such as
  `serviceDiscoveryMode=zooKeeper`, `ssl=true`, `transportMode=http` and
  `principal=...` now fail with an explicit message instead of being ignored.
- `jsonl` output and incremental JSON/JSONL writing.
- `--stream` table output with re-blocking when later rows are wider, so values
  are never silently truncated.
- `--max-column-width`, `--null-empty` and `--binary-format text|base64|hex`.
- Display-width-aware table and vertical formatting for CJK and emoji.
- REPL commands: `!describe`, `!outputformat`, `!delimiter`, `!run`/`!source`,
  `!history`, `!record` and `!dbinfo`; `!set` gained `delimiter` and
  `binaryformat`.
- E2E coverage for variables, init/force/delimiter scripts, streaming tables,
  URL rejection, password files, new formats and the new REPL commands.

## [0.1.0] - 2026-11-06

The first public preview. `hive-go` is functional and verified against real
HiveServer2 instances, but it is pre-1.0: CLI flags and the Go API may still
change, and it has not been hardened for every production environment.

### Added

- A pure-Go HiveServer2 client (`package hive`) and the `hive` CLI, with no JVM,
  Hadoop client libraries, CGO, or local Hive installation.
- Direct Thrift `TCLIService` transport with SASL PLAIN for `NONE` and `LDAP`,
  `NOSASL`, protocol V10, and V6 fallback.
- Asynchronous statement execution, status polling, and cancellation through
  the Go context or Ctrl-C, with a reusable session after cancellation.
- Result decoding for blob rowsets (`binaryColumns` via `TCompactProtocol`),
  columnar batches, and legacy row-based batches, including NULL bitmaps,
  Unicode, and embedded newlines/tabs.
- Output formats: Beeline-like table, vertical, RFC 4180 CSV, TSV, and JSON,
  with `--null-string` and `--no-header`.
- Lazy `FetchResults` paging controlled by `--fetch-size`, terminating on an
  empty batch for compatibility with Hive 3's `hasMoreRows` behavior.
- An interactive REPL with history, multi-statement input, `!` meta commands,
  and configurable output, fetch size, verbosity, timing, header, and null text.
- Batch execution with `-e`, `-f` (including `-f -`), and non-TTY stdin, plus
  stable exit codes for success, SQL/connection errors, usage errors, and
  interruption.
- `hive --version` reports the release tag for prebuilt archives and the module
  version embedded by Go for `go install` builds.
- Configuration precedence across flags, URL parameters, `HIVE_*` environment
  variables, `hive-site.xml`, and defaults, with explicit rejection of
  unsupported HTTP/SSL configurations.
- An E2E suite that runs against a real HiveServer2, creates a uniquely named
  temporary database, drops it with `CASCADE`, and writes stdout/stderr/exit,
  `report.txt`, and `report.json` artifacts.

### Verified

- Hive `3.1.0.3.1.4.0-315`, auth `NONE`, protocol V10, against a private
  endpoint.
- Apache Hive `4.0.1` in GitHub Actions, using the official `apache/hive` image
  with embedded Derby and `NONE` auth.
- 5000-row result sets at `--fetch-size 7`, columnar, row-based, and blob
  rowset batches, DECIMAL/DATE/TIMESTAMP and ARRAY/MAP/STRUCT text values,
  protocol V6, and cancellation.
- Cross-compilation for Linux, macOS, and Windows on `amd64` and `arm64` with
  `CGO_ENABLED=0`.

### Known limitations

- No Kerberos/GSSAPI, TLS, HTTP transport, or custom SASL mechanisms. LDAP
  credentials cross the network in cleartext without TLS.
- Complex type metadata is reported as a primitive `ARRAY`/`MAP`/`STRUCT`
  entry rather than the full parameterized type.
- Table, vertical, and JSON output buffer the full result set; CSV and TSV
  stream.
- No SQL parser, autocomplete, or syntax highlighting; statements are sent to
  the server as written.
- Kerberized production clusters are not supported.

### Install

```bash
go install github.com/wgzhao/hive-go/cmd/hive@v0.1.0
```

Prebuilt archives for Linux, macOS, and Windows (`amd64` and `arm64`) are
attached to the release, together with `checksums.txt`.

[Unreleased]: https://github.com/wgzhao/hive-go/compare/v0.1.0...HEAD
[0.1.0]: https://github.com/wgzhao/hive-go/releases/tag/v0.1.0
