#!/usr/bin/env bash
#
# Start a local HiveServer2 for the E2E suite (CI only).
#
# The E2E suite talks to a real server; GitHub-hosted runners cannot reach a
# private Hive endpoint, so CI runs the official Apache Hive image locally with
# an embedded Derby metastore and NONE authentication. The container is named
# $HIVE_CONTAINER so the workflow can dump its logs and remove it afterwards.
#
# Environment:
#   HIVE_IMAGE        image to run (default apache/hive:4.0.1)
#   HIVE_CONTAINER    container name (default hive-go-ci)
#   HIVE_PORT         host port to publish 10000 on (default 10000)
#   HIVE_ENGINE       hive.execution.engine (default mr; see below)
#   HIVE_CONF_HOST    host directory bind-mounted as custom Hive conf
#                     (default /tmp/hive-go-ci-conf)
#   HIVE_WAIT_SECONDS startup timeout (default 300)

set -euo pipefail

IMAGE=${HIVE_IMAGE:-apache/hive:4.0.1}
NAME=${HIVE_CONTAINER:-hive-go-ci}
PORT=${HIVE_PORT:-10000}
WAIT_SECONDS=${HIVE_WAIT_SECONDS:-300}

if ! command -v docker >/dev/null 2>&1; then
  echo "docker is required to run HiveServer2 for the E2E suite" >&2
  exit 1
fi

# The image defaults to Tez, but Tez on the container's local filesystem does
# not produce Hive's blob rowset ("serialize.in.tasks" staging files are
# missing when the fetch runs), so the E2E blob-rowset case fails. MR is
# slower but exercises the same rowset path. Override with HIVE_ENGINE=tez to
# run the default engine.
ENGINE=${HIVE_ENGINE:-mr}
# The container bind-mounts this directory, so it must outlive this script;
# stop-hive.sh removes it together with the container.
CONF_DIR=${HIVE_CONF_HOST:-/tmp/hive-go-ci-conf}
rm -rf "$CONF_DIR"
mkdir -p "$CONF_DIR"

# Start from the image's own hive-site.xml: it carries the local-filesystem,
# scratch-dir, and warehouse settings the standalone server needs. Overriding
# the engine by writing a bare hive-site.xml would drop all of them.
if ! docker run --rm --entrypoint cat "$IMAGE" /opt/hive/conf/hive-site.xml \
    > "$CONF_DIR/hive-site.xml" 2>/dev/null; then
  echo "failed to read /opt/hive/conf/hive-site.xml from $IMAGE" >&2
  exit 1
fi
awk -v engine="$ENGINE" '
  /<name>hive\.execution\.engine<\/name>/ { in_engine = 1 }
  in_engine && /<value>/ { sub(/<value>[^<]*<\/value>/, "<value>" engine "</value>"); in_engine = 0 }
  { print }
' "$CONF_DIR/hive-site.xml" > "$CONF_DIR/hive-site.xml.tmp"
mv "$CONF_DIR/hive-site.xml.tmp" "$CONF_DIR/hive-site.xml"

if ! grep -q '<name>hive.execution.engine</name>' "$CONF_DIR/hive-site.xml"; then
  sed -i "s|</configuration>|  <property>\n    <name>hive.execution.engine</name>\n    <value>${ENGINE}</value>\n  </property>\n</configuration>|" \
    "$CONF_DIR/hive-site.xml"
fi

# Leftover containers from an earlier, interrupted run would keep the port
# bound, so always start from a clean name.
docker rm -f "$NAME" >/dev/null 2>&1 || true

echo "starting $IMAGE as $NAME (host port $PORT, engine $ENGINE)"
docker run -d --name "$NAME" \
  -p "$PORT:10000" \
  -e SERVICE_NAME=hiveserver2 \
  -e DB_DRIVER=derby \
  -e HIVE_CUSTOM_CONF_DIR=/hive_custom_conf \
  -v "$CONF_DIR:/hive_custom_conf:ro" \
  "$IMAGE" >/dev/null

# Wait for the Thrift port *inside* the container. Checking from the host is
# not enough: Docker's userland proxy accepts host connections as soon as the
# port mapping exists, before HiveServer2 is listening.
echo "waiting up to ${WAIT_SECONDS}s for HiveServer2"
deadline=$((SECONDS + WAIT_SECONDS))
ready=0
while [ "$SECONDS" -lt "$deadline" ]; do
  if ! docker ps --format '{{.Names}}' | grep -qx "$NAME"; then
    echo "container exited during startup" >&2
    docker logs "$NAME" >&2 || true
    exit 1
  fi
  if docker exec "$NAME" bash -c 'exec 3<>/dev/tcp/127.0.0.1/10000' 2>/dev/null; then
    ready=1
    break
  fi
  sleep 2
done

if [ "$ready" != 1 ]; then
  echo "HiveServer2 did not become ready within ${WAIT_SECONDS}s" >&2
  docker logs --tail 200 "$NAME" >&2 || true
  exit 1
fi

# A listening socket is necessary but not sufficient: run one real query via
# the server's own client before handing the endpoint to the suite.
for _ in $(seq 1 30); do
  if docker exec "$NAME" /opt/hive/bin/beeline \
      -u "jdbc:hive2://127.0.0.1:10000/default" -n hive -e 'SELECT 1' \
      >/dev/null 2>&1; then
    echo "HiveServer2 is ready on 127.0.0.1:$PORT"
    exit 0
  fi
  sleep 2
done

echo "HiveServer2 accepted connections but did not answer a query" >&2
docker logs --tail 200 "$NAME" >&2 || true
exit 1
