# meowbot

喵宝，个人专属 Telegram 秘书。

它是一个主项目，不使用 Docker，不做网页站点，不做多用户平台。运行形态是 Go 单二进制 + SQLite + systemd user service。`modules/interest-radar` 是它的第一个后台能力模块。

## Shape

```text
cmd/meowbot                    # 唯一常驻 Telegram Bot
modules/interest-radar/cmd/radar # 兴趣资料雷达 oneshot CLI
config/radar-sources.yaml
config/radar-topics.yaml
deploy/systemd/user
scripts
```

## Commands

```text
/start
/help
/status
/note <内容>
/notes [数量]
/todo <内容>
/todos
/done <编号>
/remind <10m|2h|YYYY-MM-DD HH:MM> <内容>
/today
/backup
/why <radar条目ID>
/save <radar条目ID>
/useful <radar条目ID>
/boring <radar条目ID>
/track <方向>
/block <方向>
/more <方向>
/less <方向>
/focus <方向> <天数>
```

第一次 `/start` 会在 `TELEGRAM_ADMIN_CHAT_ID` 为空时绑定当前 chat 为管理员。

## Interest Feedback

`interest-radar` 不直接连接 Telegram，也不持有 Telegram token。日报由 `radar digest --send` 调用喵宝本机 API 发出，喵宝再把消息发送到管理员 chat。

## AI Planning

没有配置 LLM 时，radar 使用规则 planner 和规则评分，完整可运行。配置 OpenAI-compatible API 后：

```text
OPENAI_API_KEY=
OPENAI_BASE_URL=https://api.openai.com/v1
OPENAI_MODEL=
```

LLM 会参与两件事：

```text
1. Source planning
   输出严格 JSON：{"tasks":[{"query":"...","reason":"...","tools":["github_search","arxiv","rss"],"ttl_days":7}]}
   Go 校验 JSON 后再调用 GitHub/arXiv/RSS fetcher 执行搜索。

2. Candidate classification
   只对高分候选调用，输出 JSON：{"summary":"...","reason":"...","tags":["esp32","tinyml"]}
   tags 会和规则标签合并，用于后续 Telegram 反馈学习。
```

查看今天的搜索计划：

```bash
bin/radar plan --json
```

LLM 调用失败时会自动回退到规则 planner，不影响日报。

日报最多 8 条，不重复分板块灌水。发送时每条推荐直接是一张完整卡片：尽量带配图，并把标题、简介、AI 评价和反馈按钮放在同一条消息里。按钮只保留 3 个高频评价：

```text
[有用] [无聊] [收藏]
```

卡片会尽量带配图：RSS 取 feed 图片或正文首图，GitHub 项目使用 GitHub OpenGraph 图。喵宝只保存 `image_url`，不下载、不缓存图片；图片发送失败时自动退回文字卡片。

按钮回调由喵宝处理，再短暂调用 `bin/radar feedback <条目ID> <动作>` 或 `bin/radar why <条目ID>`。反馈会写入 `data/radar.db` 的 `feedback` 表，并按条目的 tags 调整 `topics` 权重：

```text
useful  +0.05 learned
boring  -0.05 learned
save    +0.10 learned
```

解释和较重的兴趣调整不塞进每条卡片里，走 Telegram 命令：`/why 273`、`/track esp32-s3 camera`、`/block crypto`、`/more tinyml`、`/less ai news`、`/focus risc-v 14`。

## Local

```bash
cp .env.example .env
$EDITOR .env
go test ./...
go run ./cmd/meowbot bot
go run ./modules/interest-radar/cmd/radar digest
```

## Mini Server

```bash
mkdir -p ~/meowbot/bin
./scripts/build.sh
./scripts/install-user-systemd.sh
```

systemd 服务：

```text
meowbot.service
interest-radar-collect.timer
interest-radar-digest.timer
interest-radar-decay.timer
interest-radar-backup.timer
```

默认数据：

```text
~/meowbot/data/meowbot.db
~/meowbot/data/radar.db
```

## Local API

喵宝提供本机 API 给其他个人模块调用。默认只监听 `127.0.0.1:8765`，需要 `MEOWBOT_API_TOKEN`。

```text
MEOWBOT_API_LISTEN_ADDR=127.0.0.1:8765
MEOWBOT_API_TOKEN=
MEOWBOT_RADAR_BIN=bin/radar
```

发送消息：

```bash
curl -H "Authorization: Bearer $MEOWBOT_API_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"text":"喵宝测试"}' \
  http://127.0.0.1:8765/v1/messages
```

### Human-in-the-loop API

Codex 或其他 AI 可以通过喵宝请求你做决定。API 只负责“提醒/询问主人”，不允许外部 AI 直接执行敏感动作。

```bash
curl -H "Authorization: Bearer $MEOWBOT_API_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "source": "codex",
    "kind": "ask",
    "level": "warning",
    "title": "部署前需要确认",
    "body": "我准备重启 meowbot.service。",
    "context": "原因：新配置已写入，需要重载服务。",
    "options": ["允许重启", "先别动"]
  }' \
  http://127.0.0.1:8765/v1/ai/events
```

响应：

```json
{"id":1,"status":"pending"}
```

你可以在 Telegram 点选项，也可以回复：

```text
/answer 1 允许重启，注意先跑测试
```

调用方查询结果：

```bash
curl -H "Authorization: Bearer $MEOWBOT_API_TOKEN" \
  http://127.0.0.1:8765/v1/ai/events/1
```

字段约定：

```text
kind: notify | ask
level: info | warning | urgent
status: notified | pending | acknowledged | snoozed | answered
```

## Evaluation

架构评价体系在 [docs/evaluation.md](docs/evaluation.md)。

```bash
./scripts/score-architecture.sh
```

目标是 `architecture_grade=S`。这个评分检查清凉边界：只允许喵宝常驻、模块不碰 Telegram token、无 Docker 运行面、统一 `.env` 自动补全、systemd timers、SQLite WAL、测试和二进制构建。
