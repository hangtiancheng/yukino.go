package manager

import (
	"strings"

	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/business/subtask"
)

// ============ Pipeline validation error hints (manager-specific) ============
//
// Pipeline data is defined in subtask (pipeline.go / pipeline_registry.go); only the correction-hint
// text used by the manager validation flow is kept here (error echo + injection for ActionRetry).

// pipelineErrorHint is the unified correction-hint prefix for pipeline validation failures.
const pipelineErrorHint = "agents_pipeline 取值不合法，必须是以下产线之一："

// listValidPipelines joins the selectable pipeline names (for error echo).
func listValidPipelines() string {
	return strings.Join(subtask.ValidPipelineNames(), " / ")
}
