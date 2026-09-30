package app

import (
	"net/http"

	"github.com/hangtiancheng/yukino.go/yukino_agent/internal/ai/agent/plan_execute_replan"
	"github.com/hangtiancheng/yukino.go/yukino_http"
)

// handleAIOps processes AI operations analysis requests.
// It runs the plan-execute-replan agent with a predefined alert analysis prompt
// and returns the final report and detailed execution steps — mirrors
// the Next.js /api/ai_ops behavior.
func (a *App) handleAIOps(ctx *yukino_http.Context, next func()) {
	appCtx := ctx.Request.Context()
	resp, detail, err := plan_execute_replan.BuildPlanAgent(appCtx, a.cfg, plan_execute_replan.AIOnOpsQuery)
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
