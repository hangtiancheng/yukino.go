package controller

import (
	"errors"

	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/creation/contracts"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/creation/dao"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/creation/service"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/pkg/response"

	yukino "github.com/hangtiancheng/yukino.go/libs/yukino_http"
)

// CreationHandler 处理创作相关请求（/inner/creation/* 与 /inner/callback/task_detail）。
//
// 直接持有 service 具体类型（*PreCreateApp 等），无接口跳转：HTTP 到业务一路可见。
type CreationHandler struct {
	preCreate   *service.PreCreateApp
	create      *service.CreateApp
	postCreate  *service.PostCreateApp
	handleError *service.HandleErrorApp
	dispatcher  *dao.CallbackDispatcher
}

// NewCreationHandler 创建 CreationHandler。
// 具体实现由 app 装配线注入（application 子用例 + dao.CallbackDispatcher）。
func NewCreationHandler(preCreate *service.PreCreateApp, create *service.CreateApp,
	postCreate *service.PostCreateApp, handleError *service.HandleErrorApp,
	dispatcher *dao.CallbackDispatcher) *CreationHandler {
	return &CreationHandler{
		preCreate:   preCreate,
		create:      create,
		postCreate:  postCreate,
		handleError: handleError,
		dispatcher:  dispatcher,
	}
}

// PreCreate 处理 /inner/creation/pre_create：输入校验 + 角色路由 + 组装下游算子入参。
func (h *CreationHandler) PreCreate(c *yukino.Context, _ func()) {
	var req contracts.PreCreateReq
	if err := c.BindJSON(&req); err != nil {
		response.Fail(c, 400, 40000, "invalid request: "+err.Error())
		return
	}

	resp, err := h.preCreate.HandlePreCreate(c.Request.Context(), &req)
	if err != nil {
		switch {
		case errors.Is(err, contracts.ErrUnsupportedRole):
			response.Fail(c, 400, 40000, err.Error())
		case errors.Is(err, contracts.ErrInvalidInput):
			response.Fail(c, 400, 40000, err.Error())
		default:
			response.Fail(c, 500, 50000, "pre_create failed")
		}
		return
	}
	// 契约 §3.2：业务错误也用 200 返回，靠 body 里 errno/status 区分。
	// 顶层就是 errno/errmsg/data（不用 response.Body 再包一层）。
	c.SetStatus(200)
	c.JSON(resp)
}

// Create 处理 /inner/creation/create：扣积分 + 建作品（幂等）。
func (h *CreationHandler) Create(c *yukino.Context, _ func()) {
	var req contracts.CreateReq
	if err := c.BindJSON(&req); err != nil {
		response.Fail(c, 400, 40000, "invalid request: "+err.Error())
		return
	}

	resp, err := h.create.HandleCreate(c.Request.Context(), &req)
	if err != nil {
		response.Fail(c, 500, 50000, "create failed")
		return
	}
	// 契约 §3.3：业务错误（积分不足）也用 200 返回，靠 data.status 区分。
	c.SetStatus(200)
	c.JSON(resp)
}

// SyncPostCreate 处理 /inner/creation/sync_post_create（妙图：同步拿结果，一步到终态）。
func (h *CreationHandler) SyncPostCreate(c *yukino.Context, _ func()) {
	var req contracts.SyncPostCreateReq
	if err := c.BindJSON(&req); err != nil {
		response.Fail(c, 400, 40000, "invalid request: "+err.Error())
		return
	}
	resp, err := h.postCreate.HandleSyncPostCreate(c.Request.Context(), &req)
	if err != nil {
		response.Fail(c, 500, 50000, "sync_post_create failed")
		return
	}
	c.SetStatus(200)
	c.JSON(resp)
}

// AsyncPostCreate 处理 /inner/creation/async_post_create（混剪/妙影/图文笔记：异步登记产线 ID）。
func (h *CreationHandler) AsyncPostCreate(c *yukino.Context, _ func()) {
	var req contracts.AsyncPostCreateReq
	if err := c.BindJSON(&req); err != nil {
		response.Fail(c, 400, 40000, "invalid request: "+err.Error())
		return
	}
	resp, err := h.postCreate.HandleAsyncPostCreate(c.Request.Context(), &req)
	if err != nil {
		response.Fail(c, 500, 50000, "async_post_create failed")
		return
	}
	c.SetStatus(200)
	c.JSON(resp)
}

// CallbackTaskDetail 处理 /inner/callback/task_detail（算子异步完成回调，HTTP 立即返回 200）。
//
// 分发逻辑在基础设施层（解析 task_type → 异步执行 handler），幂等闸门由各 handler
// 的数据层乐观锁承担（done/fail 短路），失败写补偿表待重试。
func (h *CreationHandler) CallbackTaskDetail(c *yukino.Context, _ func()) {
	var req contracts.CallbackReq
	if err := c.BindJSON(&req); err != nil {
		response.Fail(c, 400, 40000, "invalid request: "+err.Error())
		return
	}
	resp := h.dispatcher.Dispatch(c.Request.Context(), &req)
	c.SetStatus(200)
	c.JSON(resp)
}

// HandleError 处理 /inner/creation/handle_error（失败兜底回退）。
func (h *CreationHandler) HandleError(c *yukino.Context, _ func()) {
	var req contracts.HandleErrorReq
	if err := c.BindJSON(&req); err != nil {
		response.Fail(c, 400, 40000, "invalid request: "+err.Error())
		return
	}
	resp, err := h.handleError.HandleError(c.Request.Context(), &req)
	if err != nil {
		response.Fail(c, 500, 50000, "handle_error failed")
		return
	}
	c.SetStatus(200)
	c.JSON(resp)
}
