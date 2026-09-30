package handler

import (
	"github.com/hangtiancheng/yukino.go/yukino_http"
)

func JsonBack(ctx *yukino_http.Context, message string, ret int, data any) {
	ctx.Status = 200
	switch ret {
	case 0:
		resp := yukino_http.H{"code": 200, "message": message}
		if data != nil {
			resp["data"] = data
		}
		ctx.JSON(resp)
	case -2:
		ctx.JSON(yukino_http.H{"code": 400, "message": message})
	default:
		ctx.JSON(yukino_http.H{"code": 500, "message": message})
	}
}

// JsonStatus writes an explicit envelope code, for auth failures that the
// ret convention cannot express.
func JsonStatus(ctx *yukino_http.Context, code int, message string) {
	ctx.Status = 200
	ctx.JSON(yukino_http.H{"code": code, "message": message})
}
