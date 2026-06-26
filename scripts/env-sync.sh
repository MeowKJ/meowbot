#!/usr/bin/env sh
set -eu

ROOT="${ROOT:-$HOME/meowbot}"
ENV_FILE="$ROOT/.env"
EXAMPLE="$ROOT/.env.example"

if [ ! -f "$EXAMPLE" ]; then
  echo "missing $EXAMPLE" >&2
  exit 1
fi

if [ ! -f "$ENV_FILE" ]; then
  cp "$EXAMPLE" "$ENV_FILE"
fi

tmp="$(mktemp)"
cp "$ENV_FILE" "$tmp"

while IFS= read -r line; do
  case "$line" in
    ""|\#*) continue ;;
    *=*)
      key="${line%%=*}"
      if ! grep -q "^${key}=" "$tmp"; then
        printf '%s\n' "$line" >> "$tmp"
      fi
      ;;
  esac
done < "$EXAMPLE"

if grep -q '^MEOWBOT_API_TOKEN=$' "$tmp"; then
  token="$(openssl rand -hex 32 2>/dev/null || od -An -N32 -tx1 /dev/urandom | tr -d ' \n')"
  sed "s/^MEOWBOT_API_TOKEN=$/MEOWBOT_API_TOKEN=$token/" "$tmp" > "$tmp.next"
  mv "$tmp.next" "$tmp"
fi

mv "$tmp" "$ENV_FILE"
chmod 600 "$ENV_FILE"

