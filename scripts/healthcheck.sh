#!/usr/bin/env sh
set -eu

ROOT="${ROOT:-$HOME/meowbot}"

echo "== env =="
sh "$ROOT/scripts/env-doctor.sh" || true

echo "== meowbot =="
systemctl --user status meowbot.service --no-pager || true

echo "== timers =="
systemctl --user list-timers 'interest-radar-*' --no-pager || true

echo "== meowbot binary =="
"$ROOT/bin/meowbot" status || true

echo "== radar binary =="
"$ROOT/bin/radar" status || true
