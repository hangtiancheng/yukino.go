package app

import (
	"net/http"

	"github.com/hangtiancheng/yukino.go/apps/agent/server/internal/ai/agent/plan_execute_replan"
	"github.com/hangtiancheng/yukino.go/libs/yukino_http"
)

func (a *App) handleAIOps(ctx *yukino_http.Context, next func()) {
	appCtx := ctx.Request.Context()
	resp, detail, err := plan_execute_replan.BuildPlanAgent(appCtx, a.cfg, plan_execute_replan.AIOpsQuery)
	if err != nil {
		ctx.Throw(http.StatusInternalServerError, err.Error())
		return
	}
	if resp == "" {
		ctx.Throw(http.StatusInternalServerError, "internal error: empty response")
		return
	}

	data := yukino_http.H{
		"result": resp,
		"detail": detail,
	}

	ctx.Status = http.StatusOK
	ctx.JSON(yukino_http.H{
		"message": "OK",
		"data":    data,
	})
}
