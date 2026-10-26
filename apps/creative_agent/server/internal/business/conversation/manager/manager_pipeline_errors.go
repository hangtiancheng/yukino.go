package manager

import (
	"strings"

	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/business/subtask"
)

// ============ pipeline 校验错误提示（manager 专属）============
//
// 产线数据定义在 subtask（pipeline.go / pipeline_registry.go）；此处只保留
// manager 校验流程使用的纠错提示文本（报错回显 + 供 ActionRetry 注入）。

// pipelineErrorHint 是 pipeline 相关校验失败的统一纠错提示前缀。
const pipelineErrorHint = "agents_pipeline 取值不合法，必须是以下产线之一："

// listValidPipelines 拼接可选的产线名（报错回显用）。
func listValidPipelines() string {
	return strings.Join(subtask.ValidPipelineNames(), " / ")
}
