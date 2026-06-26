#!/usr/bin/env sh
set -eu

ROOT="${ROOT:-$(pwd)}"
mkdir -p "$ROOT/bin"
go build -o "$ROOT/bin/meowbot" ./cmd/meowbot
go build -o "$ROOT/bin/radar" ./modules/interest-radar/cmd/radar

