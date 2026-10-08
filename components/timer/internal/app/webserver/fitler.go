package webserver

import (
	"net/http"

	"github.com/hangtiancheng/yukino.go/components/timer/common/model/vo"

	yukino "github.com/hangtiancheng/yukino.go/libs/yukino_http"
)

// CorsHandler returns a CORS middleware.
func CorsHandler() yukino.Middleware {
	return func(ctx *yukino.Context, next func()) {
		h := ctx.Writer.Header()
		h.Set("Access-Control-Allow-Origin", "*")
		h.Set("Access-Control-Allow-Methods", "POST, GET, OPTIONS, PUT, DELETE,UPDATE")
		h.Set("Access-Control-Allow-Headers", "Authorization, Content-Length, X-CSRF-Token, Token,session,X_Requested_With,Accept, Origin, Host, Connection, Accept-Encoding, Accept-Language,DNT, X-CustomHeader, Keep-Alive, User-Agent, X-Requested-With, If-Modified-Since, Cache-Control, Content-Type, Pragma,token,openid,opentoken")
		h.Set("Access-Control-Expose-Headers", "Content-Length, Access-Control-Allow-Origin, Access-Control-Allow-Headers,Cache-Control,Content-Language,Content-Type,Expires,Last-Modified,Pragma,FooBar")
		h.Set("Access-Control-Max-Age", "172800")
		h.Set("Access-Control-Allow-Credentials", "false")
		if ctx.Request.Method == "OPTIONS" {
			ctx.SetStatus(http.StatusOK)
			ctx.JSON(&vo.CodeMsg{})
			return
		}
		next()
	}
}
