package bot

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/kong-jing/meowbot/internal/config"
	"github.com/kong-jing/meowbot/internal/db"
)

type Bot struct {
	cfg    config.Config
	store  *db.Store
	offset int64
}

func New(cfg config.Config, store *db.Store) *Bot {
	return &Bot{cfg: cfg, store: store}
}

func (b *Bot) Run(ctx context.Context) error {
	if b.cfg.TelegramToken == "" {
		return errors.New("TELEGRAM_BOT_TOKEN is empty")
	}
	if strings.TrimSpace(b.cfg.APIToken) == "" {
		return errors.New("MEOWBOT_API_TOKEN is empty")
	}
	errc := make(chan error, 3)
	go func() { errc <- b.poll(ctx) }()
	go func() { errc <- b.reminderLoop(ctx) }()
	go func() { errc <- b.apiServer(ctx) }()
	err := <-errc
	if err == context.Canceled {
		return nil
	}
	return err
}

func (b *Bot) apiServer(ctx context.Context) error {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/status", func(w http.ResponseWriter, r *http.Request) {
		if !b.authorizeAPI(w, r) {
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	})
	mux.HandleFunc("/v1/messages", func(w http.ResponseWriter, r *http.Request) {
		if !b.authorizeAPI(w, r) {
			return
		}
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var req struct {
			Text     string       `json:"text"`
			ImageURL string       `json:"image_url"`
			Actions  [][]tgButton `json:"actions"`
		}
		if err := json.NewDecoder(io.LimitReader(r.Body, 128*1024)).Decode(&req); err != nil {
			http.Error(w, "bad json", http.StatusBadRequest)
			return
		}
		req.Text = strings.TrimSpace(req.Text)
		if req.Text == "" {
			http.Error(w, "text is required", http.StatusBadRequest)
			return
		}
		chatID, ok := b.adminChatID(r.Context(), 0)
		if !ok {
			http.Error(w, "admin chat is not bound", http.StatusPreconditionFailed)
			return
		}
		if err := b.sendMessageWithMedia(r.Context(), chatID, req.Text, req.ImageURL, req.Actions); err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "sent"})
	})
	mux.HandleFunc("/v1/ai/events", func(w http.ResponseWriter, r *http.Request) {
		if !b.authorizeAPI(w, r) {
			return
		}
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		b.createAIEvent(w, r)
	})
	mux.HandleFunc("/v1/ai/events/", func(w http.ResponseWriter, r *http.Request) {
		if !b.authorizeAPI(w, r) {
			return
		}
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		b.getAIEvent(w, r)
	})
	srv := &http.Server{Addr: b.cfg.APIListenAddr, Handler: mux}
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
	}()
	err := srv.ListenAndServe()
	if err == http.ErrServerClosed {
		return nil
	}
	return err
}

func (b *Bot) createAIEvent(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Source     string   `json:"source"`
		Kind       string   `json:"kind"`
		Level      string   `json:"level"`
		Title      string   `json:"title"`
		Body       string   `json:"body"`
		Context    string   `json:"context"`
		Options    []string `json:"options"`
		ExternalID string   `json:"external_id"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 64*1024)).Decode(&req); err != nil {
		http.Error(w, "bad json", http.StatusBadRequest)
		return
	}
	ev, err := normalizeAIEventRequest(req.Source, req.Kind, req.Level, req.Title, req.Body, req.Context, req.ExternalID, req.Options)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	id, err := b.store.AddAIEvent(r.Context(), ev)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	ev.ID = id
	chatID, ok := b.adminChatID(r.Context(), 0)
	if !ok {
		http.Error(w, "admin chat is not bound", http.StatusPreconditionFailed)
		return
	}
	if err := b.sendMessageWithActions(r.Context(), chatID, formatAIEvent(ev, b.cfg.Location), aiEventActions(ev)); err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]any{"id": id, "status": ev.Status})
}

func (b *Bot) getAIEvent(w http.ResponseWriter, r *http.Request) {
	raw := strings.TrimPrefix(r.URL.Path, "/v1/ai/events/")
	id, err := strconv.ParseInt(strings.TrimSpace(raw), 10, 64)
	if err != nil || id <= 0 {
		http.Error(w, "bad event id", http.StatusBadRequest)
		return
	}
	ev, err := b.store.AIEvent(r.Context(), id)
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	_ = json.NewEncoder(w).Encode(ev)
}

func (b *Bot) authorizeAPI(w http.ResponseWriter, r *http.Request) bool {
	token := strings.TrimSpace(b.cfg.APIToken)
	if token == "" || r.Header.Get("Authorization") != "Bearer "+token {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return false
	}
	return true
}

func (b *Bot) poll(ctx context.Context) error {
	for {
		select {
		case <-ctx.Done():
			return nil
		default:
		}
		updates, err := b.getUpdates(ctx)
		if err != nil {
			time.Sleep(5 * time.Second)
			continue
		}
		for _, u := range updates {
			b.offset = u.UpdateID + 1
			_ = b.handle(ctx, u)
		}
	}
}

func (b *Bot) reminderLoop(ctx context.Context) error {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			chatID, ok := b.adminChatID(ctx, 0)
			if !ok {
				continue
			}
			due, err := b.store.DueReminders(ctx, time.Now())
			if err != nil {
				continue
			}
			for _, r := range due {
				_ = b.sendMessage(ctx, chatID, fmt.Sprintf("提醒 #%d\n%s", r.ID, r.Text))
				_ = b.store.MarkReminderSent(ctx, r.ID)
			}
		}
	}
}

func (b *Bot) handle(ctx context.Context, u update) error {
	if u.Callback.ID != "" {
		return b.handleCallback(ctx, u.Callback)
	}
	chatID := u.Message.Chat.ID
	if chatID == 0 {
		return nil
	}
	text := strings.TrimSpace(u.Message.Text)
	if strings.HasPrefix(text, "/start") && b.cfg.TelegramAdminChatID == 0 && !b.hasBoundAdmin(ctx) {
		_ = b.store.SetSetting(ctx, "admin_chat_id", strconv.FormatInt(chatID, 10))
		return b.sendMessage(ctx, chatID, "喵宝已绑定这个 chat。以后只听你的。")
	}
	if !b.allowed(ctx, chatID) {
		return b.sendMessage(ctx, chatID, "喵宝只服务主人。")
	}
	return b.command(ctx, chatID, text)
}

func (b *Bot) command(ctx context.Context, chatID int64, text string) error {
	cmd, args := splitCommand(text)
	switch cmd {
	case "/start":
		return b.sendMessage(ctx, chatID, "喵宝在。用 /help 看我会做什么。")
	case "/help":
		return b.sendMessage(ctx, chatID, helpText())
	case "/status":
		return b.sendMessage(ctx, chatID, b.store.Status(ctx))
	case "/answer":
		return b.answerAIEvent(ctx, chatID, args)
	case "/ai":
		return b.listAIEvents(ctx, chatID, args)
	case "/note":
		if args == "" {
			return b.sendMessage(ctx, chatID, "用法：/note <内容>")
		}
		id, err := b.store.AddNote(ctx, args)
		if err != nil {
			return err
		}
		return b.sendMessage(ctx, chatID, fmt.Sprintf("已记下 #%d。", id))
	case "/notes":
		limit, _ := strconv.Atoi(strings.TrimSpace(args))
		notes, err := b.store.Notes(ctx, limit)
		if err != nil {
			return err
		}
		return b.sendMessage(ctx, chatID, formatNotes(notes, b.cfg.Location))
	case "/todo":
		if args == "" {
			return b.sendMessage(ctx, chatID, "用法：/todo <内容>")
		}
		id, err := b.store.AddTodo(ctx, args)
		if err != nil {
			return err
		}
		return b.sendMessage(ctx, chatID, fmt.Sprintf("已加入待办 #%d。", id))
	case "/todos":
		todos, err := b.store.Todos(ctx)
		if err != nil {
			return err
		}
		return b.sendMessage(ctx, chatID, formatTodos(todos))
	case "/done":
		id, _ := strconv.ParseInt(strings.TrimSpace(args), 10, 64)
		ok, err := b.store.DoneTodo(ctx, id)
		if err != nil {
			return err
		}
		if !ok {
			return b.sendMessage(ctx, chatID, "没找到这个未完成待办。")
		}
		return b.sendMessage(ctx, chatID, fmt.Sprintf("完成 #%d。", id))
	case "/remind":
		dueAt, body, err := parseReminder(args, b.cfg.Location)
		if err != nil {
			return b.sendMessage(ctx, chatID, err.Error())
		}
		id, err := b.store.AddReminder(ctx, body, dueAt)
		if err != nil {
			return err
		}
		return b.sendMessage(ctx, chatID, fmt.Sprintf("已安排提醒 #%d：%s", id, dueAt.In(b.cfg.Location).Format("2006-01-02 15:04")))
	case "/today":
		return b.today(ctx, chatID)
	case "/backup":
		path, err := b.store.Backup(ctx, b.cfg.BackupDir)
		if err != nil {
			return err
		}
		return b.sendMessage(ctx, chatID, "已备份："+path)
	case "/why":
		return b.radarCommand(ctx, chatID, "why", args)
	case "/save":
		return b.radarCommand(ctx, chatID, "feedback", args, "save")
	case "/useful":
		return b.radarCommand(ctx, chatID, "feedback", args, "useful")
	case "/boring":
		return b.radarCommand(ctx, chatID, "feedback", args, "boring")
	case "/track":
		return b.radarTopicCommand(ctx, chatID, "track", args)
	case "/block":
		return b.radarTopicCommand(ctx, chatID, "block", args)
	case "/more":
		return b.radarTopicCommand(ctx, chatID, "more", args)
	case "/less":
		return b.radarTopicCommand(ctx, chatID, "less", args)
	case "/focus":
		return b.radarTopicCommand(ctx, chatID, "focus", args)
	default:
		if strings.HasPrefix(cmd, "/") {
			return b.sendMessage(ctx, chatID, "未知命令。用 /help 看列表。")
		}
		id, err := b.store.AddNote(ctx, text)
		if err != nil {
			return err
		}
		return b.sendMessage(ctx, chatID, fmt.Sprintf("默认当作笔记，已记下 #%d。", id))
	}
}

func (b *Bot) handleCallback(ctx context.Context, cb callbackQuery) error {
	chatID := cb.Message.Chat.ID
	if chatID == 0 {
		return b.answerCallbackQuery(ctx, cb.ID, "这个按钮缺少聊天上下文。")
	}
	if !b.allowed(ctx, chatID) {
		_ = b.answerCallbackQuery(ctx, cb.ID, "喵宝只服务主人。")
		return b.sendMessage(ctx, chatID, "喵宝只服务主人。")
	}
	parts := strings.Split(cb.Data, ":")
	if len(parts) >= 3 && parts[0] == "ai" {
		return b.handleAICallback(ctx, cb, parts)
	}
	if len(parts) != 3 || parts[0] != "radar" {
		return b.answerCallbackQuery(ctx, cb.ID, "未知按钮。")
	}
	action := parts[1]
	itemID := parts[2]
	switch action {
	case "why":
		out, err := b.runRadar(ctx, "why", itemID)
		if err != nil {
			_ = b.answerCallbackQuery(ctx, cb.ID, "解释失败。")
			return b.sendMessage(ctx, chatID, err.Error())
		}
		_ = b.answerCallbackQuery(ctx, cb.ID, "推荐原因已发送。")
		return b.sendMessage(ctx, chatID, out)
	case "useful", "boring", "save", "track", "block":
		out, err := b.runRadar(ctx, "feedback", itemID, action)
		if err != nil {
			return b.answerCallbackQuery(ctx, cb.ID, "反馈失败："+shortCallbackText(err.Error()))
		}
		return b.answerCallbackQuery(ctx, cb.ID, shortCallbackText(out))
	default:
		return b.answerCallbackQuery(ctx, cb.ID, "未知反馈动作。")
	}
}

func (b *Bot) handleAICallback(ctx context.Context, cb callbackQuery, parts []string) error {
	action := parts[1]
	id, err := strconv.ParseInt(parts[2], 10, 64)
	if err != nil || id <= 0 {
		return b.answerCallbackQuery(ctx, cb.ID, "坏的事件编号。")
	}
	switch action {
	case "ack":
		ok, err := b.store.SetAIEventResponse(ctx, id, "acknowledged", "已知晓")
		if err != nil || !ok {
			return b.answerCallbackQuery(ctx, cb.ID, "更新失败。")
		}
		return b.answerCallbackQuery(ctx, cb.ID, "已记录：知晓。")
	case "later":
		ok, err := b.store.SetAIEventResponse(ctx, id, "snoozed", "稍后处理")
		if err != nil || !ok {
			return b.answerCallbackQuery(ctx, cb.ID, "更新失败。")
		}
		return b.answerCallbackQuery(ctx, cb.ID, "已记录：稍后。")
	case "opt":
		if len(parts) < 4 {
			return b.answerCallbackQuery(ctx, cb.ID, "缺少选项。")
		}
		idx, err := strconv.Atoi(parts[3])
		if err != nil {
			return b.answerCallbackQuery(ctx, cb.ID, "坏的选项。")
		}
		ev, err := b.store.AIEvent(ctx, id)
		if err != nil || idx < 0 || idx >= len(ev.Options) {
			return b.answerCallbackQuery(ctx, cb.ID, "选项不存在。")
		}
		ok, err := b.store.SetAIEventResponse(ctx, id, "answered", ev.Options[idx])
		if err != nil || !ok {
			return b.answerCallbackQuery(ctx, cb.ID, "更新失败。")
		}
		return b.answerCallbackQuery(ctx, cb.ID, "已回复："+shortCallbackText(ev.Options[idx]))
	default:
		return b.answerCallbackQuery(ctx, cb.ID, "未知 AI 动作。")
	}
}

func (b *Bot) answerAIEvent(ctx context.Context, chatID int64, args string) error {
	parts := strings.Fields(args)
	if len(parts) < 2 {
		return b.sendMessage(ctx, chatID, "用法：/answer <事件ID> <你的指示>")
	}
	id, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil || id <= 0 {
		return b.sendMessage(ctx, chatID, "事件 ID 不对。")
	}
	response := strings.TrimSpace(strings.TrimPrefix(args, parts[0]))
	ok, err := b.store.SetAIEventResponse(ctx, id, "answered", response)
	if err != nil {
		return err
	}
	if !ok {
		return b.sendMessage(ctx, chatID, "没找到这个事件。")
	}
	return b.sendMessage(ctx, chatID, fmt.Sprintf("已回复 AI 事件 #%d。", id))
}

func (b *Bot) listAIEvents(ctx context.Context, chatID int64, args string) error {
	limit, _ := strconv.Atoi(strings.TrimSpace(args))
	events, err := b.store.RecentAIEvents(ctx, limit)
	if err != nil {
		return err
	}
	if len(events) == 0 {
		return b.sendMessage(ctx, chatID, "还没有 AI 事件。")
	}
	var out strings.Builder
	out.WriteString("最近 AI 事件\n")
	for _, ev := range events {
		fmt.Fprintf(&out, "#%d [%s/%s] %s - %s\n", ev.ID, ev.Kind, ev.Status, ev.Source, ev.Title)
	}
	return b.sendMessage(ctx, chatID, out.String())
}

func (b *Bot) radarCommand(ctx context.Context, chatID int64, cmd string, args ...string) error {
	var radarArgs []string
	switch cmd {
	case "why":
		id := strings.TrimSpace(firstArg(args))
		if id == "" {
			return b.sendMessage(ctx, chatID, "用法：/why <条目ID>")
		}
		radarArgs = []string{"why", id}
	case "feedback":
		id := strings.TrimSpace(firstArg(args))
		if id == "" {
			return b.sendMessage(ctx, chatID, "用法：/save|/useful|/boring <条目ID>")
		}
		action := "save"
		if len(args) > 1 {
			action = args[1]
		}
		radarArgs = []string{"feedback", id, action}
	default:
		return b.sendMessage(ctx, chatID, "未知 radar 命令。")
	}
	out, err := b.runRadar(ctx, radarArgs...)
	if err != nil {
		return b.sendMessage(ctx, chatID, err.Error())
	}
	return b.sendMessage(ctx, chatID, out)
}

func (b *Bot) radarTopicCommand(ctx context.Context, chatID int64, cmd, args string) error {
	args = strings.TrimSpace(args)
	if args == "" {
		if cmd == "focus" {
			return b.sendMessage(ctx, chatID, "用法：/focus <方向或主题> <天数>")
		}
		return b.sendMessage(ctx, chatID, "用法：/"+cmd+" <方向或主题>")
	}
	radarArgs := []string{cmd}
	radarArgs = append(radarArgs, strings.Fields(args)...)
	out, err := b.runRadar(ctx, radarArgs...)
	if err != nil {
		return b.sendMessage(ctx, chatID, err.Error())
	}
	return b.sendMessage(ctx, chatID, out)
}

func (b *Bot) runRadar(ctx context.Context, args ...string) (string, error) {
	cctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	cmd := exec.CommandContext(cctx, b.cfg.RadarBin, args...)
	out, err := cmd.CombinedOutput()
	text := strings.TrimSpace(string(out))
	if err != nil {
		if text == "" {
			text = err.Error()
		}
		return "", fmt.Errorf("radar 执行失败：%s", text)
	}
	if text == "" {
		text = "radar 已处理。"
	}
	return text, nil
}

func firstArg(args []string) string {
	if len(args) == 0 {
		return ""
	}
	return args[0]
}

func (b *Bot) today(ctx context.Context, chatID int64) error {
	todos, err := b.store.Todos(ctx)
	if err != nil {
		return err
	}
	reminders, err := b.store.UpcomingReminders(ctx, time.Now())
	if err != nil {
		return err
	}
	var out strings.Builder
	out.WriteString("今日概览\n\n")
	out.WriteString(formatTodos(todos))
	out.WriteString("\n")
	out.WriteString(formatReminders(reminders, b.cfg.Location))
	return b.sendMessage(ctx, chatID, out.String())
}

func (b *Bot) allowed(ctx context.Context, chatID int64) bool {
	admin, ok := b.adminChatID(ctx, 0)
	return ok && admin == chatID
}

func (b *Bot) hasBoundAdmin(ctx context.Context) bool {
	_, ok := b.adminChatID(ctx, 0)
	return ok
}

func (b *Bot) adminChatID(ctx context.Context, fallback int64) (int64, bool) {
	if b.cfg.TelegramAdminChatID != 0 {
		return b.cfg.TelegramAdminChatID, true
	}
	if v, ok := b.store.Setting(ctx, "admin_chat_id"); ok {
		id, err := strconv.ParseInt(v, 10, 64)
		return id, err == nil
	}
	return fallback, fallback != 0
}

func splitCommand(text string) (string, string) {
	parts := strings.Fields(text)
	if len(parts) == 0 {
		return "", ""
	}
	cmd := parts[0]
	return cmd, strings.TrimSpace(strings.TrimPrefix(text, cmd))
}

func parseReminder(args string, loc *time.Location) (time.Time, string, error) {
	args = strings.TrimSpace(args)
	parts := strings.Fields(args)
	if len(parts) < 2 {
		return time.Time{}, "", errors.New("用法：/remind <10m|2h|YYYY-MM-DD HH:MM> <内容>")
	}
	if d, err := time.ParseDuration(parts[0]); err == nil {
		body := strings.TrimSpace(strings.TrimPrefix(args, parts[0]))
		return time.Now().Add(d), body, nil
	}
	if len(parts) >= 3 {
		raw := parts[0] + " " + parts[1]
		if t, err := time.ParseInLocation("2006-01-02 15:04", raw, loc); err == nil {
			body := strings.TrimSpace(strings.TrimPrefix(args, raw))
			return t, body, nil
		}
	}
	return time.Time{}, "", errors.New("时间格式不认识。例：/remind 30m 喝水；或 /remind 2026-06-19 09:00 开会")
}

func formatNotes(notes []db.Note, loc *time.Location) string {
	if len(notes) == 0 {
		return "还没有笔记。"
	}
	var b strings.Builder
	b.WriteString("最近笔记\n")
	for _, n := range notes {
		fmt.Fprintf(&b, "#%d %s\n%s\n", n.ID, n.CreatedAt.In(loc).Format("01-02 15:04"), n.Text)
	}
	return b.String()
}

func formatTodos(todos []db.Todo) string {
	if len(todos) == 0 {
		return "待办为空。"
	}
	var b strings.Builder
	b.WriteString("待办\n")
	for _, t := range todos {
		fmt.Fprintf(&b, "#%d %s\n", t.ID, t.Text)
	}
	return b.String()
}

func formatReminders(reminders []db.Reminder, loc *time.Location) string {
	if len(reminders) == 0 {
		return "没有待提醒事项。"
	}
	var b strings.Builder
	b.WriteString("提醒\n")
	for _, r := range reminders {
		fmt.Fprintf(&b, "#%d %s %s\n", r.ID, r.DueAt.In(loc).Format("01-02 15:04"), r.Text)
	}
	return b.String()
}

func helpText() string {
	return `喵宝会这些：
/note <内容> - 记一条笔记
/notes [数量] - 看最近笔记
/todo <内容> - 加待办
/todos - 看待办
/done <编号> - 完成待办
/remind <10m|2h|YYYY-MM-DD HH:MM> <内容> - 设置提醒
/today - 今日概览
/status - 状态
/backup - 备份数据库
/ai [数量] - 查看最近 AI 事件
/answer <事件ID> <指示> - 回复 AI 请求
/why <条目ID> - 看 radar 推荐原因
/save <条目ID> - 收藏 radar 条目
/useful|/boring <条目ID> - 调整 radar 兴趣
/track|/block <方向> - 长期追踪或屏蔽方向
/more|/less <方向> - 轻微增减兴趣权重
/focus <方向> <天数> - 临时聚焦一个方向`
}

func (b *Bot) getUpdates(ctx context.Context) ([]update, error) {
	var out struct {
		OK     bool     `json:"ok"`
		Result []update `json:"result"`
	}
	err := b.api(ctx, "getUpdates", map[string]any{"timeout": 25, "offset": b.offset}, &out)
	return out.Result, err
}

func (b *Bot) sendMessage(ctx context.Context, chatID int64, text string) error {
	return b.sendMessageWithMedia(ctx, chatID, text, "", nil)
}

func (b *Bot) sendMessageWithActions(ctx context.Context, chatID int64, text string, actions [][]tgButton) error {
	return b.sendMessageWithMedia(ctx, chatID, text, "", actions)
}

func (b *Bot) sendMessageWithMedia(ctx context.Context, chatID int64, text, imageURL string, actions [][]tgButton) error {
	if strings.TrimSpace(imageURL) != "" && len([]rune(text)) <= 1000 {
		payload := map[string]any{"chat_id": chatID, "photo": imageURL, "caption": text}
		if len(actions) > 0 {
			payload["reply_markup"] = map[string]any{"inline_keyboard": actions}
		}
		var out any
		if err := b.api(ctx, "sendPhoto", payload, &out); err == nil {
			return nil
		}
	}
	const chunk = 3900
	rs := []rune(text)
	for len(rs) > 0 {
		n := len(rs)
		if n > chunk {
			n = chunk
		}
		payload := map[string]any{"chat_id": chatID, "text": string(rs[:n]), "disable_web_page_preview": true}
		if len(rs) <= chunk && len(actions) > 0 {
			payload["reply_markup"] = map[string]any{"inline_keyboard": actions}
		}
		var out any
		if err := b.api(ctx, "sendMessage", payload, &out); err != nil {
			return err
		}
		rs = rs[n:]
	}
	return nil
}

func (b *Bot) answerCallbackQuery(ctx context.Context, id, text string) error {
	var out any
	return b.api(ctx, "answerCallbackQuery", map[string]any{"callback_query_id": id, "text": shortCallbackText(text)}, &out)
}

func shortCallbackText(s string) string {
	s = strings.TrimSpace(strings.ReplaceAll(s, "\n", " "))
	rs := []rune(s)
	if len(rs) > 180 {
		return string(rs[:180])
	}
	return s
}

func (b *Bot) api(ctx context.Context, method string, payload any, out any) error {
	body, _ := json.Marshal(payload)
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.telegram.org/bot"+b.cfg.TelegramToken+"/"+method, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode >= 300 {
		b, _ := io.ReadAll(res.Body)
		return fmt.Errorf("telegram %s: %s", res.Status, strings.TrimSpace(string(b)))
	}
	return json.NewDecoder(res.Body).Decode(out)
}

type update struct {
	UpdateID int64 `json:"update_id"`
	Message  struct {
		Text string `json:"text"`
		Chat struct {
			ID int64 `json:"id"`
		} `json:"chat"`
	} `json:"message"`
	Callback callbackQuery `json:"callback_query"`
}

type callbackQuery struct {
	ID      string `json:"id"`
	Data    string `json:"data"`
	Message struct {
		Chat struct {
			ID int64 `json:"id"`
		} `json:"chat"`
	} `json:"message"`
}

type tgButton struct {
	Text         string `json:"text"`
	CallbackData string `json:"callback_data"`
}

func normalizeAIEventRequest(source, kind, level, title, body, contextText, externalID string, options []string) (db.AIEvent, error) {
	source = trimLimit(source, 80)
	kind = strings.ToLower(strings.TrimSpace(kind))
	level = strings.ToLower(strings.TrimSpace(level))
	title = trimLimit(title, 160)
	body = trimLimit(body, 3000)
	contextText = trimLimit(contextText, 3000)
	externalID = trimLimit(externalID, 160)
	if kind == "" {
		kind = "notify"
	}
	if kind != "notify" && kind != "ask" {
		return db.AIEvent{}, errors.New("kind must be notify or ask")
	}
	status := "notified"
	if kind == "ask" {
		status = "pending"
	}
	if level == "" {
		level = "info"
	}
	switch level {
	case "info", "warning", "urgent":
	default:
		return db.AIEvent{}, errors.New("level must be info, warning, or urgent")
	}
	if title == "" {
		return db.AIEvent{}, errors.New("title is required")
	}
	if body == "" {
		return db.AIEvent{}, errors.New("body is required")
	}
	var cleanOptions []string
	for _, opt := range options {
		opt = trimLimit(opt, 80)
		if opt != "" {
			cleanOptions = append(cleanOptions, opt)
		}
		if len(cleanOptions) >= 5 {
			break
		}
	}
	return db.AIEvent{Source: valueOr(source, "unknown"), Kind: kind, Level: level, Title: title, Body: body, Context: contextText, ExternalID: externalID, Status: status, Options: cleanOptions}, nil
}

func formatAIEvent(ev db.AIEvent, loc *time.Location) string {
	var b strings.Builder
	if ev.Kind == "ask" {
		fmt.Fprintf(&b, "AI 请求指示 #%d\n", ev.ID)
	} else {
		fmt.Fprintf(&b, "AI 提醒 #%d\n", ev.ID)
	}
	fmt.Fprintf(&b, "来源：%s\n级别：%s\n时间：%s\n\n", ev.Source, ev.Level, ev.CreatedAt.In(loc).Format("2006-01-02 15:04"))
	fmt.Fprintf(&b, "%s\n\n%s", ev.Title, ev.Body)
	if ev.Context != "" {
		fmt.Fprintf(&b, "\n\n上下文：\n%s", ev.Context)
	}
	if ev.Kind == "ask" {
		fmt.Fprintf(&b, "\n\n回复：/answer %d <你的指示>", ev.ID)
	}
	return b.String()
}

func aiEventActions(ev db.AIEvent) [][]tgButton {
	id := strconv.FormatInt(ev.ID, 10)
	if ev.Kind == "ask" && len(ev.Options) > 0 {
		var rows [][]tgButton
		for i, opt := range ev.Options {
			rows = append(rows, []tgButton{{Text: opt, CallbackData: "ai:opt:" + id + ":" + strconv.Itoa(i)}})
		}
		rows = append(rows, []tgButton{{Text: "稍后处理", CallbackData: "ai:later:" + id}})
		return rows
	}
	return [][]tgButton{{{Text: "已知晓", CallbackData: "ai:ack:" + id}, {Text: "稍后处理", CallbackData: "ai:later:" + id}}}
}

func trimLimit(s string, limit int) string {
	s = strings.TrimSpace(s)
	rs := []rune(s)
	if len(rs) > limit {
		return string(rs[:limit])
	}
	return s
}

func valueOr(s, fallback string) string {
	if strings.TrimSpace(s) == "" {
		return fallback
	}
	return s
}
