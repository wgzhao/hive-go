#!/usr/bin/env bash
#
# Remove the HiveServer2 container started by install-hive.sh, along with the
# host directory bind-mounted as its custom configuration.

set -uo pipefail

NAME=${HIVE_CONTAINER:-hive-go-ci}
CONF_DIR=${HIVE_CONF_HOST:-/tmp/hive-go-ci-conf}

docker rm -f "$NAME" >/dev/null 2>&1 || true
rm -rf "$CONF_DIR"
