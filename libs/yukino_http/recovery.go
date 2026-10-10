package yukino_http

import (
	"fmt"
	"log"
	"net/http"
	"runtime"
	"strings"
)

func trace(message string) string {
	var pcs [32]uintptr
	n := runtime.Callers(3, pcs[:])

	var str strings.Builder
	str.WriteString(message)
	str.WriteString("\nTraceback:")
	frames := runtime.CallersFrames(pcs[:n])
	for {
		frame, more := frames.Next()
		fmt.Fprintf(&str, "\n\t%s:%d", frame.File, frame.Line)
		if !more {
			break
		}
	}
	return str.String()
}

func Recovery() Middleware {
	return func(ctx *Context, next func()) {
		defer func() {
			if err := recover(); err != nil {
				if err == http.ErrAbortHandler {
					panic(err)
				}
				message := fmt.Sprintf("%v", err)
				log.Printf("%s\n\n", trace(message))
				ctx.Status = http.StatusInternalServerError
				ctx.statusSet = true
				ctx.Type = ""
				ctx.headers = make(map[string]string)
				ctx.Body = H{"message": "Internal Server Error"}
			}
		}()
		next()
	}
}
