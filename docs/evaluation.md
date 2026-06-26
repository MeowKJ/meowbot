# Evaluation System

This project is scored as a personal mini-server secretary system, not as a SaaS app.

## Grade Bands

S grade means quiet, bounded, and easy to trust:

- One user-facing Telegram entrypoint: `meowbot`.
- One always-on service: `meowbot.service`.
- Capability modules run as short-lived commands or timers.
- No Docker, web UI, Redis, Postgres, queue, browser automation, or multi-user surface.
- Secrets live in server-owned `.env`; deploy only reconciles missing keys.
- Modules do not read `TELEGRAM_BOT_TOKEN`.
- Module output goes through the meowbot local API.
- Data is local SQLite with WAL, backup command, and user-level systemd limits.

A grade means acceptable but heavier:

- More than one always-on local process.
- Direct module-to-Telegram delivery.
- Manual env drift, but still no secret leakage.
- No rollback, but deploy remains reliable and simple.

B grade means it can run but is not clean:

- Docker as a required runtime path.
- Web dashboards, extra databases, queues, or multi-user auth.
- Secrets copied through CI or checked into files.
- Modules mix product entrypoint logic with background collection logic.

## Scorecard

| Area | Points | S-level expectation |
|---|---:|---|
| Runtime quietness | 20 | only `meowbot.service` is always-on; modules are timers |
| Boundary clarity | 20 | meowbot owns Telegram; modules use local API |
| Deployment simplicity | 15 | build two binaries, install systemd, no server-side Docker |
| Env safety | 15 | `.env` is server-owned; sync fills missing keys only |
| Data durability | 10 | SQLite WAL plus backup command |
| Testability | 10 | `go test ./...` and both binaries build |
| Extensibility | 10 | new capabilities fit under `modules/<name>` |

Scores:

- `S`: 90-100
- `A`: 75-89
- `B`: 60-74
- `C`: below 60

Run:

```bash
./scripts/score-architecture.sh
```

