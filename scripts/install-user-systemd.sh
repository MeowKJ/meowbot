#!/usr/bin/env sh
set -eu

ROOT="${ROOT:-$HOME/meowbot}"
BIN="$ROOT/bin/meowbot"
RADAR_BIN="$ROOT/bin/radar"

cd "$ROOT"
mkdir -p "$ROOT/bin" "$ROOT/data" "$HOME/.config/systemd/user"

if [ ! -x "$BIN" ]; then
  if command -v go >/dev/null 2>&1; then
    go build -o "$BIN" ./cmd/meowbot
  else
    echo "missing $BIN and go is not installed" >&2
    exit 1
  fi
fi

if [ ! -x "$RADAR_BIN" ]; then
  if command -v go >/dev/null 2>&1; then
    go build -o "$RADAR_BIN" ./modules/interest-radar/cmd/radar
  else
    echo "missing $RADAR_BIN and go is not installed" >&2
    exit 1
  fi
fi

sh "$ROOT/scripts/env-sync.sh"
sh "$ROOT/scripts/env-doctor.sh"

cp "$ROOT"/deploy/systemd/user/*.service "$HOME/.config/systemd/user/"
cp "$ROOT"/deploy/systemd/user/*.timer "$HOME/.config/systemd/user/" 2>/dev/null || true
systemctl --user daemon-reload
systemctl --user enable meowbot.service
systemctl --user enable --now interest-radar-collect.timer interest-radar-digest.timer interest-radar-decay.timer interest-radar-backup.timer 2>/dev/null || true
systemctl --user restart meowbot.service
systemctl --user status meowbot.service --no-pager
