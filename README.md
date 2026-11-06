# hive-go

A small, pure-Go command-line client for HiveServer2, intended as a lightweight
replacement for the Java Hive CLI. The project is called **hive-go**; the
installed command is **`hive`**, mirroring `hdfs-go`'s `hdfs` binary.

It speaks the Thrift `TCLIService` protocol directly, so it does not need a JVM,
Hadoop client libraries, `hive-jdbc`, or a Hive installation on the machine
running it.

`hive` is intentionally narrower than Beeline: it connects, runs SQL, streams
result sets, supports a REPL and scripts, and prints results as tables,
vertical blocks, CSV, TSV, or JSON.

## Status

Verified end-to-end against a real HiveServer2:

- Hive `3.1.0.3.1.4.0-315`, protocol V10, auth `NONE`
- `NONE`, `NOSASL`, and `LDAP` code paths (the E2E suite in this repository was
  run with `NONE`; the other two need a matching endpoint to be exercised)
- columnar results, row-based results, and Hive's blob rowset
  (`binaryColumns` + `TCompactProtocol`)
- 5000-row result sets at `--fetch-size 7`
- NULL bitmaps, Unicode, embedded newlines/tabs, DECIMAL/DATE/TIMESTAMP,
  ARRAY/MAP/STRUCT textual values, protocol V6 fallback, and query cancellation

No unit tests are used. The verification mechanism is the E2E suite under
`cmd/hive/e2e/`, which talks to a real server and writes a repeatable
artifact directory.

## Why another client?

| Tool | Form | Pure Go | CLI | Notes |
|---|---|---|---|---|
| **hive** (this project) | HiveServer2 TCLIService client + CLI | yes | yes | No JVM/Hadoop/CGO; focused Hive UX |
| Apache Beeline | Java CLI | no | yes | The reference client; requires JVM + jars |
| `usql` + `gohive/v2` | generic SQL CLI + Go driver | yes | yes | General psql-style tool; Hive is one driver among many |
| `beltran/gohive/v2` | Go `database/sql` driver | yes | no | Library; Kerberos variant uses CGO |
| DBeaver / DataGrip | JDBC GUI | no | GUI | Heavy, JVM-dependent |

The point of this project is a single static binary that behaves like a small
Beeline for the common case: connect to HiveServer2, run SQL, read results.

## Build

Requires Go 1.22+.

```bash
make build          # produces ./hive
# or
CGO_ENABLED=0 go build -trimpath -o hive ./cmd/hive
```

`CGO_ENABLED=0` is supported, so cross-compilation is straightforward:

```bash
make cross
```

## Quick start

```bash
# NONE auth. Hive ignores the password but requires a non-empty one; the
# client supplies "x" automatically.
./hive -u hive2://127.0.0.1:10000/default -n hive \
  -e "SELECT 1 AS x, 'hello' AS s"

# Interactive REPL
./hive -u hive2://127.0.0.1:10000/default -n hive

# Script file
./hive -u hive2://127.0.0.1:10000/default -n hive -f query.sql

# Non-TTY stdin
printf 'SELECT 1;\n' | ./hive -u hive2://127.0.0.1:10000/default
```

URL forms accepted:

```
hive2://[user[:password]@]host[:port][/database]
hive://[user[:password]@]host[:port][/database]
jdbc:hive2://host:10000/default;auth=NONE
```

`;auth=...` and `?auth=...` are recognized; other JDBC properties are ignored.

## Authentication

| Mode | Flag | Behavior |
|---|---|---|
| `none` | `--auth none` (default) | SASL PLAIN; the server does not verify the password. The client sends `x` if none is given. |
| `nosasl` | `--auth nosasl` | No SASL handshake; plain Thrift binary transport. |
| `ldap` | `--auth ldap` | SASL PLAIN with the supplied username/password. |

**Security warning:** without TLS, LDAP credentials cross the network in
cleartext. Use LDAP only on a trusted network. TLS is not implemented in this
version.

Kerberos, HTTP transport, TLS, and custom SASL mechanisms are explicitly out of
scope for v1.

## Configuration

Precedence (highest first):

1. command-line flags
2. values in `--url`
3. environment variables
4. `hive-site.xml` (`HIVE_CONF_DIR`, then `$HIVE_HOME/conf`)
5. defaults (`localhost:10000`, `none`)

Environment variables:

```
HIVE_SERVER2_URL  HIVE_URL
HIVE_HOST  HIVE_PORT  HIVE_DATABASE  HIVE_USER  HIVE_PASSWORD
HIVE_AUTH  HIVE_PROTOCOL  HIVE_FETCH_SIZE
HIVE_CONF_DIR  HIVE_HOME
```

`hive-site.xml` is read for `hive.server2.thrift.port`,
`hive.server2.authentication`, `hive.server2.transport.mode`, and
`hive.server2.use.SSL`. HTTP transport or SSL being enabled is reported as an
unsupported configuration rather than silently ignored.

## Output

```bash
./hive ... --output-format table       # default, Beeline-like borders
./hive ... --output-format vertical
./hive ... --output-format csv
./hive ... --output-format tsv
./hive ... --output-format json
./hive ... --no-header --null-string '<null>'
```

- Result data goes to stdout; status, errors, info, and timing go to stderr.
- Table/vertical/TSV escape control characters visibly (`\n`, `\t`, `\r`, `\\`).
- CSV follows RFC 4180 quoting and keeps embedded newlines inside quoted fields.
- JSON preserves NULL as `null`, booleans as booleans, integer/float values as
  numbers, and binary values as base64. HiveServer2 returns DECIMAL, DATE,
  TIMESTAMP, CHAR/VARCHAR, and complex values (ARRAY/MAP/STRUCT) as strings, so
  those remain JSON strings; this avoids precision loss and matches the server
  representation.
- `--fetch-size` controls rows per `FetchResults` call (default 1000). The client
  keeps fetching until the server returns an empty batch; Hive 3 has been
  observed to return `hasMoreRows=false` while more rows exist, so
  `hasMoreRows` alone is not used as the termination condition.

## REPL

Prompt: `hive (database)> `; continuation lines use `      -> `.
History defaults to `~/.hive-go_history` (use `--no-history` to disable).

Meta commands:

```
!schemas              SHOW DATABASES
!tables [pattern]     SHOW TABLES [LIKE 'pattern']
!columns <table>      DESCRIBE <table>
!set                  show settings
!set outputformat csv
!set fetchsize 500
!set verbose on|off
!set timing on|off
!set header on|off
!set nullstring <s>
!fetchsize N
!verbose on|off
!timing on|off
!help
!quit
```

SQL statements end with `;`. Multiple statements in one line/file work. The
splitter understands single/double quotes, backticks, `--` comments, `/* */`
comments, backslash escapes, and doubled quotes.

Ctrl-C cancels the running query through `CancelOperation` and returns to the
prompt. In batch mode it exits with status 130. The session remains usable after
cancellation.

## Exit codes

| Code | Meaning |
|---|---|
| 0 | success |
| 1 | SQL, connection, or I/O error |
| 2 | usage/configuration error |
| 130 | interrupted/canceled |

## E2E tests

The suite creates a uniquely named temporary database, runs medium-to-hard
cases against the real server, and drops the database at the end. It refuses to
run unless `HIVE_ALLOW_DDL=1` is set.

```bash
HIVE_HOST=127.0.0.1 \
HIVE_PORT=10000 \
HIVE_AUTH=none \
HIVE_USER=hive \
HIVE_ALLOW_DDL=1 \
make e2e
```

Replace `HIVE_HOST`/`HIVE_PORT` with your endpoint. In the verified
environment, the Hive user must be `hive` (or another user with write access to
the HDFS warehouse); the default OS user cannot create tables in the managed
warehouse. The test database is created with `LOCATION '/tmp/<db>'` so the test
user owns the data and `DROP DATABASE ... CASCADE` removes it.

`E2E_SLOW=1` adds the cancellation case. `UPDATE_GOLDEN=1` regenerates golden
files from the current server output.

Artifacts are written to `cmd/hive/e2e/artifacts/<UTC timestamp>_<pid>/`:

```
env.txt            host/port/auth/user/version, no password
commands.log       commands executed
cases/<name>/      cmd, stdout, stderr, exit, diff/failure
report.txt         PASS/FAIL summary
report.json        machine-readable summary
cleanup.stdout     cleanup result (including remaining_databases=0)
```

## Protocol notes

The implementation uses Apache Thrift `TCLIService` generated from Hive 4.0.0's
IDL and the Thrift Go runtime `v0.19.0`.

- `OpenSession` requests protocol V10; `--protocol v6` forces V6.
- NONE/LDAP perform a SASL PLAIN handshake (Thrift status byte + 4-byte
  big-endian length framing), then reuse the stock `TFramedTransport`.
- NOSASL uses `TBufferedTransport` with no SASL framing.
- Query execution is asynchronous; the client polls `GetOperationStatus` with
  backoff and cancels via `CancelOperation` when the context is canceled.
- Result batches are decoded in this order: blob `binaryColumns`
  (TCompactProtocol), columnar `columns`, then legacy `rows`.
- NULL bitmaps use `nulls[i/8] & (1 << (i%8)) != 0`.
- Hive returns TIMESTAMP, DATE, DECIMAL, CHAR/VARCHAR, INTERVAL, ARRAY, MAP, and
  STRUCT values as strings in practice; the client prints them as returned.

## Code generation

Generated files are committed, so a normal build does not need the Thrift
compiler. To regenerate:

```bash
sudo apt-get install thrift-compiler   # 0.19.0
make generate
```

The IDL is `internal/tcliservice/TCLIService.thrift` (Apache Hive 4.0.0).

## Limitations

- No Kerberos/GSSAPI, HTTP transport, TLS, or custom SASL mechanisms.
- Complex type metadata from HiveServer2 is a primitive `ARRAY`/`MAP`/`STRUCT`
  entry, so the CLI displays those names rather than `ARRAY<STRING>`.
- Table/vertical/JSON outputs buffer the full result set; CSV/TSV stream.
- No SQL parser, autocomplete, or syntax highlighting; statements are sent to
  the server as written.
- No Kerberos means this cannot connect to most Kerberized production clusters.

## License

MIT. See `LICENSE`. The TCLIService IDL is Apache-2.0; see `NOTICE`.
