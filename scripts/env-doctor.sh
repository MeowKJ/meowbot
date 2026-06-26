#!/usr/bin/env sh
set -eu

ROOT="${ROOT:-$HOME/meowbot}"
ENV_FILE="$ROOT/.env"

if [ ! -f "$ENV_FILE" ]; then
  echo "missing .env"
  exit 1
fi

missing=0
need_keys="TELEGRAM_BOT_TOKEN MEOWBOT_API_TOKEN MEOWBOT_API_URL MEOWBOT_API_LISTEN_ADDR MEOWBOT_DB_PATH RADAR_DB_PATH RADAR_SOURCES_FILE RADAR_TOPICS_FILE"

for key in $need_keys; do
  if ! grep -q "^${key}=." "$ENV_FILE"; then
    echo "missing $key"
    missing=1
  fi
done

if [ "$missing" -eq 0 ]; then
  echo "env ok"
fi

exit "$missing"
