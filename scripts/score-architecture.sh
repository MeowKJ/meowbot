#!/usr/bin/env sh
set -eu

ROOT="${ROOT:-$(pwd)}"
cd "$ROOT"

score=100
failures=""
warnings=""

penalty() {
  points="$1"
  msg="$2"
  score=$((score - points))
  failures="${failures}- ${msg}\n"
}

warn() {
  msg="$1"
  warnings="${warnings}- ${msg}\n"
}

exists() {
  [ -e "$1" ]
}

contains() {
  pattern="$1"
  path="$2"
  grep -RInE "$pattern" "$path" >/dev/null 2>&1
}

if find . -path ./.git -prune -o \( -name 'Dockerfile' -o -name 'docker-compose.yml' -o -name 'compose*.yml' \) -print | grep -q .; then
  penalty 20 "Docker runtime files are present"
fi

if contains 'docker compose|Dockerfile|compose build|docker run' README.md scripts deploy .github 2>/dev/null; then
  penalty 10 "Docker runtime instructions remain in deployment or docs"
fi

if ! exists cmd/meowbot/main.go; then
  penalty 10 "missing cmd/meowbot"
fi

if ! exists modules/interest-radar/cmd/radar/main.go; then
  penalty 10 "missing modules/interest-radar/cmd/radar"
fi

always_on="$(grep -Rl 'WantedBy=default.target' deploy/systemd/user/*.service 2>/dev/null | wc -l | tr -d ' ')"
if [ "$always_on" -ne 1 ]; then
  penalty 10 "expected exactly one always-on service"
fi

if ! exists deploy/systemd/user/meowbot.service; then
  penalty 10 "missing meowbot.service"
fi

for t in collect digest decay backup; do
  if ! exists "deploy/systemd/user/interest-radar-${t}.timer"; then
    penalty 5 "missing interest-radar-${t}.timer"
  fi
done

if grep -RIn 'TELEGRAM_BOT_TOKEN' modules >/dev/null 2>&1; then
  penalty 20 "module code references TELEGRAM_BOT_TOKEN"
fi

if ! grep -RIn 'MEOWBOT_API_TOKEN' modules/interest-radar >/dev/null 2>&1; then
  penalty 10 "interest-radar does not use meowbot API token"
fi

for f in scripts/env-sync.sh scripts/env-doctor.sh scripts/build.sh scripts/install-user-systemd.sh; do
  if ! exists "$f"; then
    penalty 5 "missing $f"
  fi
done

if ! grep -q '^MEOWBOT_API_TOKEN=' .env.example; then
  penalty 5 ".env.example missing MEOWBOT_API_TOKEN"
fi

if ! grep -q '^RADAR_DB_PATH=' .env.example; then
  penalty 5 ".env.example missing RADAR_DB_PATH"
fi

if ! grep -RIn 'journal_mode=WAL' internal modules >/dev/null 2>&1; then
  penalty 5 "SQLite WAL not detected"
fi

if ! go test ./... >/tmp/meowbot-score-go-test.log 2>&1; then
  penalty 10 "go test ./... failed"
fi

if ! go build -o /tmp/meowbot-score-meowbot ./cmd/meowbot >/tmp/meowbot-score-build-meowbot.log 2>&1; then
  penalty 5 "meowbot binary build failed"
fi

if ! go build -o /tmp/meowbot-score-radar ./modules/interest-radar/cmd/radar >/tmp/meowbot-score-build-radar.log 2>&1; then
  penalty 5 "radar binary build failed"
fi

if grep -RIn 'User-Agent.*radar-bot' modules >/dev/null 2>&1; then
  warn "legacy radar-bot user-agent string remains"
fi

if [ "$score" -lt 0 ]; then
  score=0
fi

grade="C"
if [ "$score" -ge 90 ]; then
  grade="S"
elif [ "$score" -ge 75 ]; then
  grade="A"
elif [ "$score" -ge 60 ]; then
  grade="B"
fi

printf 'architecture_score=%s\n' "$score"
printf 'architecture_grade=%s\n' "$grade"

if [ -n "$failures" ]; then
  printf '\nfailures:\n%b' "$failures"
fi

if [ -n "$warnings" ]; then
  printf '\nwarnings:\n%b' "$warnings"
fi

test "$grade" = "S"
