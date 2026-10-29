package response

import (
	yukino "github.com/hangtiancheng/yukino.go/libs/yukino_http"
)

// Body is the unified JSON response structure.
type Body struct {
	Code    int         `json:"code"`
	Message string      `json:"message"`
	Data    interface{} `json:"data,omitempty"`
}

// OK returns a success response with HTTP status 200.
func OK(ctx *yukino.Context, data interface{}) {
	ctx.SetStatus(200)
	ctx.JSON(Body{Code: 0, Message: "success", Data: data})
}

// Fail returns a failure response; httpStatus is the HTTP status code and code is the business error code.
func Fail(ctx *yukino.Context, httpStatus, code int, message string) {
	ctx.SetStatus(httpStatus)
	ctx.JSON(Body{Code: code, Message: message})
}
