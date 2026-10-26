package domain

// --- 风控子产品（subProduct）策略路由键 ---
//
// 命名规范：dujiaapp_{功能}_{模态}_{input|output}。
// 关键理解：同一段内容在「输入」和「产出」两个阶段用不同的 subProduct（_input vs _output），
// 因为输入审的是"用户想生成什么"，产出审的是"模型实际生成了什么"，审核策略跟着流程阶段走。
// 完整子产品全集见上游 library/audit/types.go。

const (
	// 混剪 mix_video
	MixVideoT2IInputSubproduct       = "dujiaapp_mixvideo_t2t_input"                // 编剧阶段-脚本文本输入
	MixVideoT2IOutputSubproduct      = "dujiaapp_mixvideoscriptgenerate_t2t_output" // 编剧阶段-脚本输出
	MixVideoT2VInputSubproduct       = "dujiaapp_mixvideoscript_t2t_input"          // 成片阶段-文本输入
	MixVideo60sT2VOutputSubproduct   = "dujiaapp_mixvideo60s_t2v_output"            // 成片阶段-视频输出(60s)
	MixVideo120sT2VOutputSubproduct  = "dujiaapp_mixvideo120s_t2v_output"           // 成片阶段-视频输出(120s)
	MixVideoTitleT2TOutputSubproduct = "dujiaapp_mixvideotitle_t2t_output"          // 成片阶段-标题输出

	// 妙图 miaotu
	MiaotuImageTextInputSubproduct = "dujiaapp_miaotu_t2i_input"
	MiaotuImageOutputSubproduct    = "dujiaapp_miaotu_t2i_output"

	// 妙影 miaoying
	MiaoyingVideoTextInputSubproduct = "dujiaapp_miaoying5s_t2v_input"
	MiaoyingT2VOutputSubproduct      = "dujiaapp_miaoying5s_t2v_output"

	// 图文笔记 graphic_notes
	GraphicNotesT2TInputSubproduct  = "dujiaapp_graphicnotes_t2i_input"
	GraphicNotesT2IInputSubproduct  = "dujiaapp_graphicnotesscript_t2i_input"
	GraphicNotesT2IOutputSubproduct = "dujiaapp_graphicnotes_t2i_output"

	// 脑洞 brainstorm
	BrainstormPlanInputSubproduct = "dujiaapp_brainstormplan_t2t_input"
)
