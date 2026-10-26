package response

import (
	yukino "github.com/hangtiancheng/yukino.go/libs/yukino_http"
)

// Body 是统一的 JSON 响应结构。
type Body struct {
	Code    int         `json:"code"`
	Message string      `json:"message"`
	Data    interface{} `json:"data,omitempty"`
}

// OK 返回成功响应，HTTP 状态码 200。
func OK(ctx *yukino.Context, data interface{}) {
	ctx.SetStatus(200)
	ctx.JSON(Body{Code: 0, Message: "success", Data: data})
}

// Fail 返回失败响应，httpStatus 为 HTTP 状态码，code 为业务错误码。
func Fail(ctx *yukino.Context, httpStatus, code int, message string) {
	ctx.SetStatus(httpStatus)
	ctx.JSON(Body{Code: code, Message: message})
}
