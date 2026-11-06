package main

import (
	"fmt"
	"io"
)

const usageText = `Usage: hive [OPTIONS] [SQL]

A pure-Go HiveServer2 command-line client and replacement for the Java Hive CLI.

Connection options:
  -u, --url URL             hive2://[user[:pass]@]host[:port][/db]
                            (hive:// and jdbc:hive2:// prefixes are accepted)
  -n, --username NAME       username (default: current OS user)
  -p, --password PASS       password (prefer HIVE_PASSWORD or the prompt)
      --auth MODE           none, nosasl or ldap (default: none)
      --protocol VERSION    auto, v10 or v6 (default: auto)
      --host HOST           server host (default: localhost)
      --port PORT           server port (default: 10000)
  -d, --database DB         database to USE after connecting
      --connect-timeout D   connection timeout (default: 10s)
      --socket-timeout D    per-RPC socket timeout (default: none)
      --query-timeout N     server query timeout in seconds (default: 0)

Execution options:
  -e, --execute SQL         execute SQL and exit
  -f, --file FILE           execute statements from FILE ('-' for stdin)
      --fetch-size N        rows per FetchResults call (default: 1000)
      --verbose             print connection and statement details
      --timing              print per-statement timing
      --silent              suppress non-result status messages

Output options:
      --output-format FMT   table, vertical, csv, tsv or json (default: table)
      --null-string STR     text used for NULL (default: NULL)
      --no-header           omit the header row for tabular output

REPL options:
      --history-file FILE   history file (default: ~/.hive-go_history)
      --no-history          disable history

Other:
  -h, --help                print this help and exit
  -v, --version             print the version and exit

Environment:
  HIVE_SERVER2_URL, HIVE_HOST, HIVE_PORT, HIVE_DATABASE, HIVE_USER,
  HIVE_PASSWORD, HIVE_AUTH, HIVE_PROTOCOL, HIVE_FETCH_SIZE,
  HIVE_CONF_DIR, HIVE_HOME

Exit codes:
  0 success, 1 SQL/connection error, 2 usage error, 130 interrupted
`

func printUsage(w io.Writer) {
	fmt.Fprint(w, usageText)
}
