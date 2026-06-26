package bot

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/kong-jing/meowbot/internal/config"
)

func TestParseReminderDuration(t *testing.T) {
	due, body, err := parseReminder("10m stretch", time.Local)
	if err != nil {
		t.Fatal(err)
	}
	if body != "stretch" {
		t.Fatalf("body=%q", body)
	}
	if time.Until(due) < 9*time.Minute {
		t.Fatalf("due too soon: %v", due)
	}
}

func TestParseReminderAbsolute(t *testing.T) {
	loc := time.FixedZone("T", 8*3600)
	due, body, err := parseReminder("2026-06-19 09:00 meeting", loc)
	if err != nil {
		t.Fatal(err)
	}
	if due.In(loc).Format("2006-01-02 15:04") != "2026-06-19 09:00" || body != "meeting" {
		t.Fatalf("bad parse: %v %q", due, body)
	}
}

func TestNormalizeAIEventRequest(t *testing.T) {
	ev, err := normalizeAIEventRequest("codex", "ask", "urgent", "Build failed", "CI failed at test step", "main branch", "run-123", []string{"重试", "先别动"})
	if err != nil {
		t.Fatal(err)
	}
	if ev.Kind != "ask" || ev.Level != "urgent" || len(ev.Options) != 2 {
		t.Fatalf("bad event: %+v", ev)
	}
	if ev.Status != "pending" {
		t.Fatalf("status=%q", ev.Status)
	}
}

func TestNormalizeAIEventRejectsBadKind(t *testing.T) {
	_, err := normalizeAIEventRequest("codex", "execute", "info", "x", "y", "", "", nil)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestAuthorizeAPIRejectsEmptyConfiguredToken(t *testing.T) {
	b := New(config.Config{APIToken: ""}, nil)
	req := httptest.NewRequest(http.MethodGet, "/v1/status", nil)
	req.Header.Set("Authorization", "Bearer ")
	rr := httptest.NewRecorder()
	if b.authorizeAPI(rr, req) {
		t.Fatal("expected empty configured token to be rejected")
	}
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d", rr.Code)
	}
}

func TestAuthorizeAPIRejectsWrongToken(t *testing.T) {
	b := New(config.Config{APIToken: "secret"}, nil)
	req := httptest.NewRequest(http.MethodGet, "/v1/status", nil)
	req.Header.Set("Authorization", "Bearer wrong")
	rr := httptest.NewRecorder()
	if b.authorizeAPI(rr, req) {
		t.Fatal("expected wrong token to be rejected")
	}
}

func TestAuthorizeAPIAcceptsConfiguredToken(t *testing.T) {
	b := New(config.Config{APIToken: "secret"}, nil)
	req := httptest.NewRequest(http.MethodGet, "/v1/status", nil)
	req.Header.Set("Authorization", "Bearer secret")
	rr := httptest.NewRecorder()
	if !b.authorizeAPI(rr, req) {
		t.Fatal("expected configured token to be accepted")
	}
}
