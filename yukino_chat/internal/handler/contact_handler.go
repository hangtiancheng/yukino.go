package handler

import (
	"github.com/hangtiancheng/yukino.go/yukino_chat/internal/service"

	"github.com/hangtiancheng/yukino.go/yukino_http"
)

func GetUserList(ctx *yukino_http.Context, next func()) {
	var req struct {
		OwnerId string `json:"owner_id"`
	}
	if err := ctx.BindJSON(&req); err != nil {
		JsonBack(ctx, "invalid request body", -1, nil)
		return
	}
	msg, data, ret := service.GetUserList(ctx.Request.Context(), req.OwnerId)
	JsonBack(ctx, msg, ret, data)
}

func GetTagList(ctx *yukino_http.Context, next func()) {
	var req struct {
		OwnerId string `json:"owner_id"`
	}
	if err := ctx.BindJSON(&req); err != nil {
		JsonBack(ctx, "invalid request body", -1, nil)
		return
	}
	msg, data, ret := service.GetTagList(ctx.Request.Context(), req.OwnerId)
	JsonBack(ctx, msg, ret, data)
}

func AddTag(ctx *yukino_http.Context, next func()) {
	var req struct {
		OwnerId string `json:"owner_id"`
		Name    string `json:"name"`
	}
	if err := ctx.BindJSON(&req); err != nil {
		JsonBack(ctx, "invalid request body", -1, nil)
		return
	}
	msg, data, ret := service.AddTag(ctx.Request.Context(), req.OwnerId, req.Name)
	JsonBack(ctx, msg, ret, data)
}

func UpdateContact(ctx *yukino_http.Context, next func()) {
	var req struct {
		UserId    string  `json:"user_id"`
		ContactId string  `json:"contact_id"`
		NoteName  *string `json:"note_name"`
		TagId     *string `json:"tag_id"`
	}
	if err := ctx.BindJSON(&req); err != nil {
		JsonBack(ctx, "invalid request body", -1, nil)
		return
	}
	msg, ret := service.UpdateContact(ctx.Request.Context(), req.UserId, req.ContactId, req.NoteName, req.TagId)
	JsonBack(ctx, msg, ret, nil)
}

func LoadMyJoinedGroup(ctx *yukino_http.Context, next func()) {
	var req struct {
		OwnerId string `json:"owner_id"`
	}
	if err := ctx.BindJSON(&req); err != nil {
		JsonBack(ctx, "invalid request body", -1, nil)
		return
	}
	msg, data, ret := service.LoadMyJoinedGroup(ctx.Request.Context(), req.OwnerId)
	JsonBack(ctx, msg, ret, data)
}

func GetContactInfo(ctx *yukino_http.Context, next func()) {
	var req struct {
		UserId    string `json:"user_id"`
		ContactId string `json:"contact_id"`
	}
	if err := ctx.BindJSON(&req); err != nil {
		JsonBack(ctx, "invalid request body", -1, nil)
		return
	}
	msg, data, ret := service.GetContactInfo(ctx.Request.Context(), req.UserId, req.ContactId)
	JsonBack(ctx, msg, ret, data)
}

func ApplyContact(ctx *yukino_http.Context, next func()) {
	var req struct {
		UserId      string `json:"user_id"`
		ContactId   string `json:"contact_id"`
		ContactType int8   `json:"contact_type"`
		Message     string `json:"message"`
	}
	if err := ctx.BindJSON(&req); err != nil {
		JsonBack(ctx, "invalid request body", -1, nil)
		return
	}
	msg, ret := service.ApplyContact(ctx.Request.Context(), req.UserId, req.ContactId, req.ContactType, req.Message)
	JsonBack(ctx, msg, ret, nil)
}

func GetNewContactList(ctx *yukino_http.Context, next func()) {
	var req struct {
		UserId string `json:"user_id"`
	}
	if err := ctx.BindJSON(&req); err != nil {
		JsonBack(ctx, "invalid request body", -1, nil)
		return
	}
	msg, data, ret := service.GetNewContactList(ctx.Request.Context(), req.UserId)
	JsonBack(ctx, msg, ret, data)
}

func PassContactApply(ctx *yukino_http.Context, next func()) {
	var req struct {
		ApplyId string `json:"apply_id"`
	}
	if err := ctx.BindJSON(&req); err != nil {
		JsonBack(ctx, "invalid request body", -1, nil)
		return
	}
	msg, ret := service.PassContactApply(ctx.Request.Context(), req.ApplyId)
	JsonBack(ctx, msg, ret, nil)
}

func BlackContact(ctx *yukino_http.Context, next func()) {
	var req struct {
		UserId    string `json:"user_id"`
		ContactId string `json:"contact_id"`
	}
	if err := ctx.BindJSON(&req); err != nil {
		JsonBack(ctx, "invalid request body", -1, nil)
		return
	}
	msg, ret := service.BlackContact(ctx.Request.Context(), req.UserId, req.ContactId)
	JsonBack(ctx, msg, ret, nil)
}

func CancelBlackContact(ctx *yukino_http.Context, next func()) {
	var req struct {
		UserId    string `json:"user_id"`
		ContactId string `json:"contact_id"`
	}
	if err := ctx.BindJSON(&req); err != nil {
		JsonBack(ctx, "invalid request body", -1, nil)
		return
	}
	msg, ret := service.CancelBlackContact(ctx.Request.Context(), req.UserId, req.ContactId)
	JsonBack(ctx, msg, ret, nil)
}

func DeleteContact(ctx *yukino_http.Context, next func()) {
	var req struct {
		UserId    string `json:"user_id"`
		ContactId string `json:"contact_id"`
	}
	if err := ctx.BindJSON(&req); err != nil {
		JsonBack(ctx, "invalid request body", -1, nil)
		return
	}
	msg, ret := service.DeleteContact(ctx.Request.Context(), req.UserId, req.ContactId)
	JsonBack(ctx, msg, ret, nil)
}

func RefuseContactApply(ctx *yukino_http.Context, next func()) {
	var req struct {
		ApplyId string `json:"apply_id"`
	}
	if err := ctx.BindJSON(&req); err != nil {
		JsonBack(ctx, "invalid request body", -1, nil)
		return
	}
	msg, ret := service.RefuseContactApply(ctx.Request.Context(), req.ApplyId)
	JsonBack(ctx, msg, ret, nil)
}

func BlackApply(ctx *yukino_http.Context, next func()) {
	var req struct {
		ApplyId string `json:"apply_id"`
	}
	if err := ctx.BindJSON(&req); err != nil {
		JsonBack(ctx, "invalid request body", -1, nil)
		return
	}
	msg, ret := service.BlackApply(ctx.Request.Context(), req.ApplyId)
	JsonBack(ctx, msg, ret, nil)
}

func GetAddGroupList(ctx *yukino_http.Context, next func()) {
	var req struct {
		UserId string `json:"user_id"`
	}
	if err := ctx.BindJSON(&req); err != nil {
		JsonBack(ctx, "invalid request body", -1, nil)
		return
	}
	msg, data, ret := service.GetAddGroupList(ctx.Request.Context(), req.UserId)
	JsonBack(ctx, msg, ret, data)
}
