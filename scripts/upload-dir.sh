#!/usr/bin/env bash
# Upload every regular file in a directory to POST /api/files.
# Subdirectories and hidden files are skipped.

set -euo pipefail

if [[ $# -ne 1 || ! -d $1 ]]; then
  echo "usage: $0 <directory>" >&2
  exit 2
fi

dir=$(cd "$1" && pwd)
base=${RAG_API_URL:-http://127.0.0.1:8080}
base=${base%/}
url="${base}/api/files"

mapfile -d '' files < <(find "$dir" -mindepth 1 -maxdepth 1 -type f ! -name '.*' -print0 | sort -z)

if [[ ${#files[@]} -eq 0 ]]; then
  echo "no files in $dir" >&2
  exit 1
fi

fail=0
for path in "${files[@]}"; do
  name=$(basename "$path")
  mime=application/octet-stream
  lower=${name,,}
  case $lower in
    *.txt | *.md) mime=text/plain ;;
    *.json) mime=application/json ;;
    *.pdf) mime=application/pdf ;;
  esac
  body=$(mktemp)
  code=$(curl -sS -o "$body" -w '%{http_code}' -F "file=@${path};type=${mime};filename=${name}" "$url" || true)
  printf '%s %s\n' "$code" "$name"
  if [[ $code != 200 && $code != 201 ]]; then
    cat "$body" >&2 || true
    fail=1
  fi
  rm -f "$body"
done

exit "$fail"
