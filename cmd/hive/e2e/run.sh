#!/usr/bin/env bash
#
# End-to-end tests for the hive CLI (project hive-go) against a real HiveServer2.
#
# Required environment:
#   HIVE_HOST=127.0.0.1
#   HIVE_PORT=10000
#   HIVE_AUTH=none|nosasl|ldap
#   HIVE_USER=hive
#   HIVE_ALLOW_DDL=1
#
# Optional:
#   HIVE_PASSWORD=...      (never written to artifacts)
#   HIVE_CONF_DIR=...      (otherwise the client defaults are used)
#   E2E_SLOW=1             (run the cancellation case)
#   UPDATE_GOLDEN=1        (rewrite golden files from this run)
#   HIVE_BIN=...           (use a prebuilt binary)

set -uo pipefail

E2E_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
ROOT=$(cd "$E2E_DIR/../../.." && pwd)
BIN=${HIVE_BIN:-${HIVE_GO_BIN:-$ROOT/hive}}
GOLDEN="$E2E_DIR/golden"

HOST=${HIVE_HOST:-127.0.0.1}
PORT=${HIVE_PORT:-10000}
AUTH=${HIVE_AUTH:-none}
USER=${HIVE_USER:-hive}
ALLOW_DDL=${HIVE_ALLOW_DDL:-0}
SLOW=${E2E_SLOW:-0}

if [ "$ALLOW_DDL" != "1" ]; then
  echo "HIVE_ALLOW_DDL=1 is required: this test creates and drops a temporary database." >&2
  exit 2
fi

if [ -n "${HIVE_PASSWORD:-}" ]; then
  export HIVE_PASSWORD
fi

if [ ! -x "$BIN" ]; then
  echo "building hive..." >&2
  (cd "$ROOT" && GOTOOLCHAIN=local CGO_ENABLED=0 go build -trimpath -o hive ./cmd/hive) || exit 1
fi

TS=$(date -u +%Y-%m-%dT%H-%M-%SZ)
ARTIFACTS="$E2E_DIR/artifacts/${TS}_$$"
mkdir -p "$ARTIFACTS/cases" "$ARTIFACTS/sql" "$GOLDEN"
COMMANDS_LOG="$ARTIFACTS/commands.log"
: > "$COMMANDS_LOG"

DB="hive_go_e2e_$(date -u +%Y%m%d%H%M%S)_$$"
LOC="/tmp/$DB"
SETUP_OK=0
CLEANED=0

PASS=0
FAIL=0
SKIP=0
: > "$ARTIFACTS/report.tsv"

hive_cmd() {
  "$BIN" -u "hive2://$HOST:$PORT/default" -n "$USER" --auth "$AUTH" "$@"
}

log_cmd() {
  printf '%s\n' "$*" >> "$COMMANDS_LOG"
}

record() {
  local name=$1 status=$2 code=$3
  printf '%s\t%s\t%s\n' "$name" "$status" "$code" >> "$ARTIFACTS/report.tsv"
  case "$status" in
    PASS) PASS=$((PASS + 1)) ;;
    FAIL) FAIL=$((FAIL + 1)) ;;
    SKIP) SKIP=$((SKIP + 1)) ;;
  esac
  printf '%-5s %-24s exit=%s\n' "$status" "$name" "$code" | tee -a "$ARTIFACTS/report.txt"
}

cleanup() {
  [ "$CLEANED" = 1 ] && return
  CLEANED=1
  trap - EXIT INT TERM
  if [ "$SETUP_OK" = 1 ]; then
    hive_cmd -e "DROP DATABASE IF EXISTS \`$DB\` CASCADE" \
      >"$ARTIFACTS/cleanup.stdout" 2>"$ARTIFACTS/cleanup.stderr" || true
    local left
    left=$(hive_cmd --output-format csv --no-header -e "SHOW DATABASES LIKE '$DB'" 2>>"$ARTIFACTS/cleanup.stderr" | wc -l)
    printf 'cleanup: remaining_databases=%s\n' "$left" >> "$ARTIFACTS/cleanup.stdout"
    if command -v hdfs >/dev/null 2>&1 && hdfs ls "$LOC" >/dev/null 2>&1; then
      hdfs rm -r "$LOC" >>"$ARTIFACTS/cleanup.stdout" 2>>"$ARTIFACTS/cleanup.stderr" || true
    fi
  fi
  {
    printf 'summary: PASS=%d FAIL=%d SKIP=%d\n' "$PASS" "$FAIL" "$SKIP"
    printf 'artifacts: %s\n' "$ARTIFACTS"
    printf 'database: %s\n' "$DB"
  } >> "$ARTIFACTS/report.txt"

  # report.json (names and statuses are controlled, so simple escaping is fine)
  {
    printf '{\n  "timestamp": "%s",\n  "host": "%s",\n  "port": %s,\n' "$TS" "$HOST" "$PORT"
    printf '  "auth": "%s",\n  "user": "%s",\n  "database": "%s",\n' "$AUTH" "$USER" "$DB"
    printf '  "pass": %d,\n  "fail": %d,\n  "skip": %d,\n' "$PASS" "$FAIL" "$SKIP"
    printf '  "cases": [\n'
    local first=1 name status code
    while IFS=$'\t' read -r name status code; do
      [ -z "${name:-}" ] && continue
      [ "$first" = 1 ] || printf ',\n'
      first=0
      printf '    {"name": "%s", "status": "%s", "exit": %s}' "$name" "$status" "${code:-0}"
    done < "$ARTIFACTS/report.tsv"
    printf '\n  ]\n}\n'
  } > "$ARTIFACTS/report.json"

  if [ "$FAIL" -ne 0 ]; then
    exit 1
  fi
  exit 0
}
trap cleanup EXIT INT TERM

# ---------------------------------------------------------------- setup

log_cmd "CREATE DATABASE \`$DB\` LOCATION '$LOC'"
if ! hive_cmd -e "CREATE DATABASE \`$DB\` LOCATION '$LOC'" >"$ARTIFACTS/setup.stdout" 2>"$ARTIFACTS/setup.stderr"; then
  echo "failed to create test database $DB" >&2
  cat "$ARTIFACTS/setup.stderr" >&2
  SETUP_OK=0
  FAIL=$((FAIL + 1))
  printf 'FAIL\tsetup\t1\n' >> "$ARTIFACTS/report.tsv"
  printf 'FAIL  setup\n' | tee -a "$ARTIFACTS/report.txt"
  exit 1
fi
SETUP_OK=1

log_cmd "CREATE TABLE e2e_types (id INT, s STRING, d DOUBLE) STORED AS ORC"
if ! hive_cmd -d "$DB" -e "CREATE TABLE e2e_types (id INT, s STRING, d DOUBLE) STORED AS ORC" \
  >>"$ARTIFACTS/setup.stdout" 2>>"$ARTIFACTS/setup.stderr"; then
  echo "failed to create e2e_types" >&2
  exit 1
fi

cat > "$ARTIFACTS/sql/types_insert.sql" <<'SQL'
INSERT INTO e2e_types VALUES
(0,NULL,0.0),
(1,'v1',1.1),
(2,'v2',2.2),
(3,'v3',3.3),
(4,'v4',4.4),
(5,'v5',5.5),
(6,'v6',6.6),
(7,NULL,7.7),
(8,NULL,8.8),
(9,NULL,9.9),
(10,'v10',10.1),
(11,'v11',11.11),
(12,'v12',12.12),
(13,'v13',13.13),
(14,'v14',14.14),
(15,NULL,15.15),
(16,NULL,16.16),
(17,'中文😀',17.17),
(18,'line1\nline2',18.18),
(19,'tab\there',19.19),
(20,"quote's",20.20),
(21,'back\\slash',21.21);
SQL

log_cmd "LOAD types_insert.sql"
if ! hive_cmd -d "$DB" -f "$ARTIFACTS/sql/types_insert.sql" \
  >>"$ARTIFACTS/setup.stdout" 2>>"$ARTIFACTS/setup.stderr"; then
  echo "failed to insert e2e_types rows" >&2
  exit 1
fi

# Server version for env.txt; not part of the pass/fail cases.
hive_cmd --output-format csv --no-header -e "SELECT version()" >"$ARTIFACTS/server_version.txt" 2>>"$ARTIFACTS/setup.stderr" || true
{
  printf 'timestamp=%s\n' "$TS"
  printf 'host=%s\nport=%s\nauth=%s\nuser=%s\n' "$HOST" "$PORT" "$AUTH" "$USER"
  printf 'hive=%s\n' "$("$BIN" --version)"
  printf 'go=%s\n' "$(go version 2>/dev/null || echo unknown)"
  printf 'server=%s\n' "$(tr -d '\n' < "$ARTIFACTS/server_version.txt" 2>/dev/null || true)"
  printf 'database=%s\n' "$DB"
} > "$ARTIFACTS/env.txt"

# ---------------------------------------------------------------- helpers

run_case() {
  local name=$1 expected=$2 snippet=$3
  local dir="$ARTIFACTS/cases/$name"
  mkdir -p "$dir"
  printf '%s\n' "$snippet" > "$dir/cmd"
  log_cmd "$snippet"
  ( eval "$snippet" ) >"$dir/stdout" 2>"$dir/stderr"
  local code=$?
  printf '%s\n' "$code" > "$dir/exit"
  if [ "$code" -ne "$expected" ]; then
    printf 'expected exit %s, got %s\n' "$expected" "$code" >> "$dir/failure"
    record "$name" FAIL "$code"
    return 1
  fi
  return 0
}

golden_check() {
  local name=$1
  local dir="$ARTIFACTS/cases/$name"
  sed -e "s|$DB|hive_go_e2e|g" "$dir/stdout" > "$dir/stdout.normalized"
  if [ "${UPDATE_GOLDEN:-0}" = "1" ]; then
    cp "$dir/stdout.normalized" "$GOLDEN/$name.txt"
    record "$name" PASS 0
    return 0
  fi
  if diff -u "$GOLDEN/$name.txt" "$dir/stdout.normalized" > "$dir/diff"; then
    record "$name" PASS 0
    return 0
  fi
  record "$name" FAIL 0
  return 1
}

run_golden() {
  local name=$1 snippet=$2
  if run_case "$name" 0 "$snippet"; then
    golden_check "$name"
  fi
}

# ---------------------------------------------------------------- cases

run_golden basic_table \
  "hive_cmd -e \"SELECT 1 AS x, 'a' AS s, true AS b, NULL AS n\""
run_golden basic_vertical \
  "hive_cmd --output-format vertical -e \"SELECT 1 AS x, 'a' AS s, true AS b, NULL AS n\""
run_golden basic_csv \
  "hive_cmd --output-format csv -e \"SELECT 1 AS x, 'a' AS s, true AS b, NULL AS n\""
run_golden basic_tsv \
  "hive_cmd --output-format tsv -e \"SELECT 1 AS x, 'a' AS s, true AS b, NULL AS n\""
run_golden basic_json \
  "hive_cmd --output-format json -e \"SELECT 1 AS x, 'a' AS s, true AS b, NULL AS n\""

TYPE_QUERY="SELECT CAST(1 AS TINYINT) AS ti, CAST(2 AS SMALLINT) AS si, CAST(3 AS INT) AS i, CAST(4 AS BIGINT) AS bi, CAST(5.5 AS FLOAT) AS f, CAST(6.25 AS DOUBLE) AS d, CAST(7.125 AS DECIMAL(10,3)) AS decv, CAST('2024-01-02' AS DATE) AS dt, CAST('2024-01-02 03:04:05' AS TIMESTAMP) AS ts, CAST('abc' AS CHAR(5)) AS ch, CAST('abcdef' AS VARCHAR(4)) AS vc, array('a','b') AS arr, map('k', 1) AS m, named_struct('x', 1, 'y', 'z') AS st, CAST('bin' AS BINARY) AS binv"
run_golden types_table "hive_cmd -e \"$TYPE_QUERY\""
run_golden types_json "hive_cmd --output-format json -e \"$TYPE_QUERY\""

cat > "$ARTIFACTS/sql/multi.sql" <<'SQL'
-- leading line comment; this semicolon must not split
SELECT 1 AS a;
/* block comment ; with a semicolon */
SELECT ';' AS semi;
SELECT 2 AS b
SQL
run_golden multi_script "hive_cmd -f '$ARTIFACTS/sql/multi.sql'"

run_golden ddl_types \
  "hive_cmd -d '$DB' --fetch-size 7 -e 'SELECT id, s, d FROM e2e_types ORDER BY id'"

run_case meta_commands 0 \
  "printf '!set outputformat csv\n!tables\n!columns e2e_types\n!quit\n' | hive_cmd -d '$DB'"
if [ -f "$ARTIFACTS/cases/meta_commands/exit" ] && [ "$(cat "$ARTIFACTS/cases/meta_commands/exit")" = 0 ]; then
  golden_check meta_commands
fi

# 5000 rows at fetch-size 7; the array is generated locally.
run_case paging 0 \
  "hive_cmd --fetch-size 7 --output-format csv --no-header -e \"SELECT explode(array(\$(seq -s, 0 4999))) AS n\""
if [ -f "$ARTIFACTS/cases/paging/exit" ] && [ "$(cat "$ARTIFACTS/cases/paging/exit")" = 0 ]; then
  pdir="$ARTIFACTS/cases/paging"
  lines=$(wc -l < "$pdir/stdout")
  first=$(head -1 "$pdir/stdout")
  last=$(tail -1 "$pdir/stdout")
  sum=$(awk '{s+=$1} END {printf "%.0f", s}' "$pdir/stdout")
  if [ "$lines" = 5000 ] && [ "$first" = 0 ] && [ "$last" = 4999 ] && [ "$sum" = 12497500 ]; then
    record paging PASS 0
  else
    printf 'paging mismatch: lines=%s first=%s last=%s sum=%s\n' "$lines" "$first" "$last" "$sum" >> "$pdir/failure"
    record paging FAIL 0
  fi
fi

run_case error_table 1 "hive_cmd -e 'SELECT * FROM no_such_table_e2e'"
if [ "$(cat "$ARTIFACTS/cases/error_table/exit" 2>/dev/null)" = 1 ]; then
  if grep -q 'Table not found' "$ARTIFACTS/cases/error_table/stderr" &&
     grep -q 'sqlState=42S02' "$ARTIFACTS/cases/error_table/stderr"; then
    record error_table PASS 1
  else
    record error_table FAIL 1
  fi
fi

run_case error_flag 2 "'$BIN' --no-such-flag"
if [ "$(cat "$ARTIFACTS/cases/error_flag/exit" 2>/dev/null)" = 2 ] &&
   grep -q 'unknown option' "$ARTIFACTS/cases/error_flag/stderr"; then
  record error_flag PASS 2
else
  record error_flag FAIL 2
fi

mkdir -p "$ARTIFACTS/hive-site"
cat > "$ARTIFACTS/hive-site/hive-site.xml" <<EOF
<?xml version="1.0"?>
<configuration>
  <property><name>hive.server2.thrift.port</name><value>$PORT</value></property>
  <property><name>hive.server2.authentication</name><value>$AUTH</value></property>
  <property><name>hive.server2.transport.mode</name><value>binary</value></property>
</configuration>
EOF
run_golden hive_site \
  "HIVE_CONF_DIR='$ARTIFACTS/hive-site' '$BIN' --host '$HOST' -n '$USER' -e 'SELECT 1 AS site'"

run_golden protocol_v6 "hive_cmd --protocol v6 -e 'SELECT 1 AS x'"

run_case blob_rowset 0 \
  "hive_cmd -d '$DB' --fetch-size 2 --verbose -e 'SET hive.server2.thrift.resultset.serialize.in.tasks=true; SELECT id, s FROM e2e_types WHERE id IN (0,1,2) ORDER BY id'"
if [ "$(cat "$ARTIFACTS/cases/blob_rowset/exit" 2>/dev/null)" = 0 ]; then
  if grep -qE 'blob=[1-9]' "$ARTIFACTS/cases/blob_rowset/stderr"; then
    golden_check blob_rowset
  else
    printf 'blob rowset was not used\n' >> "$ARTIFACTS/cases/blob_rowset/failure"
    record blob_rowset FAIL 0
  fi
fi

if [ "$SLOW" = "1" ]; then
  cdir="$ARTIFACTS/cases/cancel"
  mkdir -p "$cdir"
  (
    "$BIN" -u "hive2://$HOST:$PORT/default" -n "$USER" --auth "$AUTH" -d "$DB" \
      -e "SELECT count(*) FROM e2e_types a CROSS JOIN e2e_types b CROSS JOIN e2e_types c CROSS JOIN e2e_types d" \
      >"$cdir/stdout" 2>"$cdir/stderr" &
    pid=$!
    sleep 1
    kill -INT "$pid" 2>/dev/null || true
    wait "$pid"
    echo $? > "$cdir/exit"
  )
  code=$(cat "$cdir/exit")
  if [ "$code" = 130 ] && grep -q 'Query canceled' "$cdir/stderr"; then
    record cancel PASS 130
  else
    record cancel FAIL "$code"
  fi
else
  record cancel SKIP 0
fi

# ---------------------------------------------------------------- done

if [ "$FAIL" -ne 0 ]; then
  echo "E2E FAILED: PASS=$PASS FAIL=$FAIL SKIP=$SKIP" >&2
  echo "artifacts: $ARTIFACTS" >&2
  exit 1
fi
echo "E2E PASSED: PASS=$PASS FAIL=$FAIL SKIP=$SKIP"
echo "artifacts: $ARTIFACTS"
exit 0
