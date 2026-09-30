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
