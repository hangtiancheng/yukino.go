package controller

import (
	"errors"

	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/creation/contracts"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/creation/dao"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/creation/service"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/pkg/response"

	yukino "github.com/hangtiancheng/yukino.go/libs/yukino_http"
)

// CreationHandler handles creation-related requests (/inner/creation/* and /inner/callback/task_detail).
//
// It holds concrete service types (*PreCreateApp, etc.) directly, with no interface hops: HTTP to business is visible all the way.
type CreationHandler struct {
	preCreate   *service.PreCreateApp
	create      *service.CreateApp
	postCreate  *service.PostCreateApp
	handleError *service.HandleErrorApp
	dispatcher  *dao.CallbackDispatcher
}

// NewCreationHandler creates a CreationHandler.
// Concrete implementations are injected by the app assembly line (application sub-use-cases + dao.CallbackDispatcher).
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

// PreCreate handles /inner/creation/pre_create: input validation + role routing + assembling downstream operator inputs.
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
	// Contract §3.2: business errors also return 200, distinguished by errno/status in the body.
	// The top level is errno/errmsg/data (no extra response.Body wrapper).
	c.SetStatus(200)
	c.JSON(resp)
}

// Create handles /inner/creation/create: deduct points + create the work (idempotent).
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
	// Contract §3.3: business errors (insufficient points) also return 200, distinguished by data.status.
	c.SetStatus(200)
	c.JSON(resp)
}

// SyncPostCreate handles /inner/creation/sync_post_create (instant-image: get the result synchronously, one step to terminal).
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

// AsyncPostCreate handles /inner/creation/async_post_create (mix-cut/instant-video/image-text article: register the pipeline ID asynchronously).
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

// CallbackTaskDetail handles /inner/callback/task_detail (the operator async-completion callback; HTTP returns 200 immediately).
//
// Dispatch logic lives in the infrastructure layer (parse task_type → execute the handler asynchronously); the idempotency gate is carried by each handler's
// data-layer optimistic lock (done/fail short-circuit), with failures written to the compensation table for retry.
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

// HandleError handles /inner/creation/handle_error (failure fallback rollback).
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
