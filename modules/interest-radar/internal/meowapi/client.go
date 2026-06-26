package meowapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

type Client struct {
	baseURL string
	token   string
}

type Button struct {
	Text         string `json:"text"`
	CallbackData string `json:"callback_data"`
}

func New(baseURL, token string) *Client {
	return &Client{baseURL: strings.TrimRight(baseURL, "/"), token: token}
}

func (c *Client) Enabled() bool {
	return c != nil && c.baseURL != "" && c.token != ""
}

func (c *Client) SendMessage(ctx context.Context, text string) error {
	return c.SendMessageWithActions(ctx, text, nil)
}

func (c *Client) SendMessageWithActions(ctx context.Context, text string, actions [][]Button) error {
	return c.SendMessageWithMedia(ctx, text, "", actions)
}

func (c *Client) SendMessageWithMedia(ctx context.Context, text, imageURL string, actions [][]Button) error {
	if !c.Enabled() {
		return fmt.Errorf("MEOWBOT_API_URL or MEOWBOT_API_TOKEN is empty")
	}
	body, _ := json.Marshal(struct {
		Text     string     `json:"text"`
		ImageURL string     `json:"image_url,omitempty"`
		Actions  [][]Button `json:"actions,omitempty"`
	}{Text: text, ImageURL: imageURL, Actions: actions})
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/v1/messages", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.token)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode >= 300 {
		b, _ := io.ReadAll(res.Body)
		return fmt.Errorf("meowbot api %s: %s", res.Status, strings.TrimSpace(string(b)))
	}
	return nil
}
