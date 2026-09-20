// Copyright (c) 2026 hangtiancheng
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.

package webserver

import (
	"fmt"
	"net/http/httptest"
	"sync"

	"github.com/hangtiancheng/yukino.go/apps/timer_demo/common/conf"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	yukino "github.com/hangtiancheng/yukino.go/yukino_http"
)

type Server struct {
	sync.Once
	app *yukino.Application

	timerApp *TimerApp
	taskApp  *TaskApp

	timerRouter *yukino.Router
	taskRouter  *yukino.Router
	mockRouter  *yukino.Router

	confProvider *conf.WebServerAppConfProvider
}

func NewServer(timer *TimerApp, task *TaskApp, confProvider *conf.WebServerAppConfProvider) *Server {
	s := Server{
		app:          yukino.Default(),
		timerApp:     timer,
		taskApp:      task,
		confProvider: confProvider,
	}

	s.app.Use(CorsHandler())

	s.timerRouter = s.app.Router("/api/timer/v1")
	s.taskRouter = s.app.Router("/api/task/v1")
	s.mockRouter = s.app.Router("/api/mock/v1")
	s.RegisterMockRouter()
	s.RegisterTimerRouter()
	s.RegisterTaskRouter()
	s.RegisterMonitorRouter()
	return &s
}

func (s *Server) Start() {
	s.Do(s.start)
}

func (s *Server) start() {
	conf := s.confProvider.Get()
	go func() {
		if err := s.app.Listen(fmt.Sprintf(":%d", conf.Port)); err != nil {
			panic(err)
		}
	}()
}

func (s *Server) RegisterTimerRouter() {
	s.timerRouter.Get("/def", s.timerApp.GetTimer)
	s.timerRouter.Post("/def", s.timerApp.CreateTimer)
	s.timerRouter.Delete("/def", s.timerApp.DeleteTimer)
	s.timerRouter.Patch("/def", s.timerApp.UpdateTimer)

	s.timerRouter.Get("/defs", s.timerApp.GetAppTimers)
	s.timerRouter.Get("/defsByName", s.timerApp.GetTimersByName)

	s.timerRouter.Post("/enable", s.timerApp.EnableTimer)
	s.timerRouter.Post("/unable", s.timerApp.UnableTimer)
}

func (s *Server) RegisterTaskRouter() {
	s.taskRouter.Get("/records", s.taskApp.GetTasks)
}

func (s *Server) RegisterMockRouter() {
	s.mockRouter.All("/mock", func(ctx *yukino.Context, next func()) {
		ctx.JSON(struct {
			Word string `json:"word"`
		}{
			Word: "hello world!",
		})
	})
}

func (s *Server) RegisterMonitorRouter() {
	s.app.All("/metrics", func(ctx *yukino.Context, next func()) {
		rec := httptest.NewRecorder()
		promhttp.Handler().ServeHTTP(rec, ctx.Request)
		for k, vs := range rec.Header() {
			for _, v := range vs {
				ctx.Writer.Header().Add(k, v)
			}
		}
		ctx.SetStatus(rec.Code)
		ctx.Data(rec.Body.Bytes())
	})
}
