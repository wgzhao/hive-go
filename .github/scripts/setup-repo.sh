#!/usr/bin/env bash
#
# Apply the public GitHub metadata for hive-go: description, topics, community
# switches, and private vulnerability reporting.
#
# Usage:
#   .github/scripts/setup-repo.sh [OWNER/REPO]
#
# Social preview images cannot be uploaded through the GitHub API. After running
# this script, upload docs/social-preview.png in:
#   Settings -> General -> Social preview -> Edit -> Upload an image
#
set -euo pipefail

repo="${1:-wgzhao/hive-go}"

if ! command -v gh >/dev/null 2>&1; then
  echo "gh is required: https://cli.github.com/" >&2
  exit 1
fi
if ! gh auth status >/dev/null 2>&1; then
  echo "gh is not authenticated; run 'gh auth login' first." >&2
  exit 1
fi

gh repo edit "$repo" \
  --description "Pure-Go HiveServer2 client and CLI — query Apache Hive over Thrift without a JVM, Hadoop client libraries, or CGO." \
  --add-topic hive,hiveserver2,apache-hive,hive-cli,golang,go,thrift,cli,sql,big-data,hadoop,beeline,database,pure-go \
  --enable-discussions \
  --enable-wiki=false

# Enable the private reporting form linked from SECURITY.md.
gh api --method PUT "repos/$repo/private-vulnerability-reporting" >/dev/null

cat <<'EOF'

Repository metadata updated.

Remaining manual step (not supported by the GitHub API):
  Settings -> General -> Social preview -> Edit
  Upload docs/social-preview.png (1280x640, under 1 MB).
EOF
