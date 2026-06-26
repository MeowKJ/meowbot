package planner

import (
	"sort"
	"strings"
	"time"

	"github.com/kong-jing/meowbot/modules/interest-radar/internal/model"
)

func Generate(topics []model.Topic, now time.Time) []model.ExplorationTask {
	active := topics[:0]
	for _, t := range topics {
		if t.Source == "blocked" || t.Weight <= 0 {
			continue
		}
		active = append(active, t)
	}
	sort.Slice(active, func(i, j int) bool { return active[i].Weight > active[j].Weight })
	var tasks []model.ExplorationTask
	add := func(query, reason string, tools []string) {
		tasks = append(tasks, model.ExplorationTask{
			Query:     query,
			Reason:    reason,
			Tools:     tools,
			Status:    "planned",
			CreatedAt: now,
			ExpiresAt: now.AddDate(0, 0, 7),
		})
	}
	for _, t := range active {
		if len(tasks) >= 8 {
			break
		}
		key := t.Name
		if len(t.Keywords) > 0 {
			key = strings.Join(t.Keywords[:min(2, len(t.Keywords))], " ")
		}
		add(key+" open source demo GitHub", "主题 "+t.Name+" 当前权重较高", []string{"github_search", "rss"})
		if len(tasks) >= 8 {
			break
		}
		if strings.Contains(key, "ai") || strings.Contains(key, "robot") || strings.Contains(key, "rl") || strings.Contains(key, "tinyml") {
			add(key+" recent paper reproducible code", "结合论文和可复现代码寻找可玩项目", []string{"arxiv", "github_search"})
		}
	}
	if len(tasks) < 5 {
		add("ESP32-S3 TinyML camera open source", "默认探索：边缘 AI 与开源硬件交叉", []string{"github_search", "arxiv", "rss"})
		add("reinforcement learning game environment open source", "默认探索：游戏 AI / 自博弈兴趣", []string{"github_search", "arxiv"})
		add("RISC-V robotics sensor open hardware", "默认探索：RISC-V 与机器人硬件", []string{"github_search", "rss"})
	}
	return tasks[:min(10, len(tasks))]
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
