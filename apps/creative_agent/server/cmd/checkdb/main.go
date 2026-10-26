package main

import (
	"fmt"
	"log"
	"os"

	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/config"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/model"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/pkg/otelx"
	"go.uber.org/zap"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
)

func main() {
	if len(os.Args) < 2 {
		log.Fatal("usage: checkdb <session_id>")
	}
	session := os.Args[1]
	cfg := config.Load("config/config.json")
	db, err := gorm.Open(mysql.Open(cfg.MySQL.DSN()),
		&gorm.Config{Logger: otelx.NewGormLogger(zap.NewNop(), 0)})
	if err != nil {
		log.Fatal("connect:", err)
	}
	var runs []model.CGTaskRun
	db.Where("session_id = ?", session).Order("task_run_id ASC").Find(&runs)
	for _, r := range runs {
		var steps []model.CGTaskStep
		db.Where("task_run_id = ?", r.TaskRunID).Where("parent_step_id = 0").Order("task_step_id").Find(&steps)
		for _, s := range steps {
			inter := s.IntermediateResult
			if len(inter) > 110 {
				inter = inter[:110] + "..."
			}
			fmt.Printf("run=%d status=%d | step=%s status=%d need_wait=%d | %s\n",
				r.TaskRunID, r.Status, s.StepName, s.Status, s.NeedWait, inter)
		}
	}
}
