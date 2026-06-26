package config

import (
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	DBPath              string
	BackupDir           string
	Timezone            string
	APIListenAddr       string
	APIToken            string
	RadarBin            string
	TelegramToken       string
	TelegramAdminChatID int64
	Location            *time.Location
}

func Load() (Config, error) {
	tz := env("MEOWBOT_TIMEZONE", "Asia/Shanghai")
	loc, err := time.LoadLocation(tz)
	if err != nil {
		return Config{}, err
	}
	return Config{
		DBPath:              env("MEOWBOT_DB_PATH", "data/meowbot.db"),
		BackupDir:           env("MEOWBOT_BACKUP_DIR", "data/backups"),
		Timezone:            tz,
		APIListenAddr:       env("MEOWBOT_API_LISTEN_ADDR", "127.0.0.1:8765"),
		APIToken:            os.Getenv("MEOWBOT_API_TOKEN"),
		RadarBin:            env("MEOWBOT_RADAR_BIN", "bin/radar"),
		TelegramToken:       os.Getenv("TELEGRAM_BOT_TOKEN"),
		TelegramAdminChatID: int64Value(os.Getenv("TELEGRAM_ADMIN_CHAT_ID")),
		Location:            loc,
	}, nil
}

func env(k, def string) string {
	if v := os.Getenv(k); strings.TrimSpace(v) != "" {
		return v
	}
	return def
}

func int64Value(s string) int64 {
	v, _ := strconv.ParseInt(strings.TrimSpace(s), 10, 64)
	return v
}
