package main

import (
	"net/http"
	_ "net/http/pprof"
	"os"
	"os/signal"
	"syscall"

	"github.com/hangtiancheng/yukino.go/components/timer/internal/app"
)

func main() {
	migratorApp := app.GetMigratorApp()
	schedulerApp := app.GetSchedulerApp()
	webServer := app.GetWebServer()
	monitor := app.GetMonitorApp()

	migratorApp.Start()
	defer migratorApp.Stop()

	schedulerApp.Start()
	defer schedulerApp.Stop()

	monitor.Start()
	defer monitor.Stop()

	webServer.Start()

	// pprof server
	go func() {
		http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {})
		_ = http.ListenAndServe(":9999", nil)
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT)
	<-quit
}
