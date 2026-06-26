package planner

import (
	"testing"
	"time"

	"github.com/kong-jing/meowbot/modules/interest-radar/internal/model"
)

func TestGenerateUsesWeightedTopics(t *testing.T) {
	tasks := Generate([]model.Topic{{Name: "tinyml", Weight: 1, Keywords: []string{"tinyml"}}}, time.Now())
	if len(tasks) == 0 || tasks[0].Query == "" || len(tasks[0].Tools) == 0 {
		t.Fatalf("bad tasks: %+v", tasks)
	}
}
