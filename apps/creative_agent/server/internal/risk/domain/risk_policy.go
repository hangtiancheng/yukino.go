package domain

// --- Risk sub-product (subProduct) policy routing keys ---
//
// Naming convention: dujiaapp_{feature}_{modality}_{input|output}.
// Key insight: the same content uses different subProducts at the "input" and "output" stages (_input vs _output),
// because input audits "what the user wants to generate" while output audits "what the model actually generated"; audit policies follow the flow stage.
// See the upstream library/audit/types.go for the full sub-product set.

const (
	// Mix-cut (mix_video)
	MixVideoT2IInputSubproduct       = "dujiaapp_mixvideo_t2t_input"                // scriptwriter stage - script text input
	MixVideoT2IOutputSubproduct      = "dujiaapp_mixvideoscriptgenerate_t2t_output" // scriptwriter stage - script output
	MixVideoT2VInputSubproduct       = "dujiaapp_mixvideoscript_t2t_input"          // final-video stage - text input
	MixVideo60sT2VOutputSubproduct   = "dujiaapp_mixvideo60s_t2v_output"            // final-video stage - video output (60s)
	MixVideo120sT2VOutputSubproduct  = "dujiaapp_mixvideo120s_t2v_output"           // final-video stage - video output (120s)
	MixVideoTitleT2TOutputSubproduct = "dujiaapp_mixvideotitle_t2t_output"          // final-video stage - title output

	// Instant image (miaotu)
	MiaotuImageTextInputSubproduct = "dujiaapp_miaotu_t2i_input"
	MiaotuImageOutputSubproduct    = "dujiaapp_miaotu_t2i_output"

	// Instant video (miaoying)
	MiaoyingVideoTextInputSubproduct = "dujiaapp_miaoying5s_t2v_input"
	MiaoyingT2VOutputSubproduct      = "dujiaapp_miaoying5s_t2v_output"

	// Image-text article (graphic_notes)
	GraphicNotesT2TInputSubproduct  = "dujiaapp_graphicnotes_t2i_input"
	GraphicNotesT2IInputSubproduct  = "dujiaapp_graphicnotesscript_t2i_input"
	GraphicNotesT2IOutputSubproduct = "dujiaapp_graphicnotes_t2i_output"

	// Brainstorm
	BrainstormPlanInputSubproduct = "dujiaapp_brainstormplan_t2t_input"
)
