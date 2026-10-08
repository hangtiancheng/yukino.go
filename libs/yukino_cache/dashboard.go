package yukino_cache

import (
	"context"
	"log"
	"sync"
	"time"

	"github.com/hangtiancheng/yukino.go/libs/yukino_http"
)

type dashboardSnapshot struct {
	Type   string          `json:"type"`
	Groups []groupSnapshot `json:"groups"`
}

type groupSnapshot struct {
	Name    string          `json:"name"`
	Stats   map[string]any  `json:"stats"`
	Entries []entrySnapshot `json:"entries"`
}

type entrySnapshot struct {
	Key      string `json:"key"`
	Size     int    `json:"size"`
	ExpireAt int64  `json:"expire_at"`
	Level    int    `json:"level"`
}

type dashboardCommand struct {
	Action string `json:"action"`
	Group  string `json:"group"`
	Key    string `json:"key"`
}

var dashboardOnce sync.Once

// DashboardHandler returns a handler that can be mounted on any yukino_http application
// to serve the dashboard WebSocket endpoint.
func DashboardHandler() func(ctx *yukino_http.Context, next func()) {
	return func(ctx *yukino_http.Context, next func()) {
		ws, err := ctx.Upgrade(&yukino_http.UpgradeOptions{
			ReadBufferSize:  4096,
			WriteBufferSize: 4096,
		})
		if err != nil {
			log.Printf("[Dashboard] upgrade failed: %v", err)
			return
		}

		serveDashboardConn(ws)
	}
}

func serveDashboardConn(ws *yukino_http.WSConn) {
	stopHeartbeat := ws.Heartbeat(30 * time.Second)

	go func() {
		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()
		defer ws.Close()
		defer stopHeartbeat()

		if err := ws.WriteJSON(buildSnapshot()); err != nil {
			return
		}

		for {
			select {
			case <-ticker.C:
				if err := ws.WriteJSON(buildSnapshot()); err != nil {
					return
				}
			case <-ws.Closed():
				return
			}
		}
	}()

	go func() {
		for {
			var cmd dashboardCommand
			if err := ws.ReadJSON(&cmd); err != nil {
				break
			}
			handleCommand(cmd)
		}
	}()
}

// StartDashboard starts the dashboard HTTP server on addr.
// It is safe to call multiple times; only the first call takes effect.
func StartDashboard(addr string) {
	dashboardOnce.Do(func() {
		app := yukino_http.New()
		app.Get("/dashboard/ws", DashboardHandler())

		go func() {
			log.Printf("[Dashboard] listening on %s", addr)
			if err := app.Listen(addr); err != nil {
				log.Printf("[Dashboard] server error: %v", err)
			}
		}()
	})
}

func buildSnapshot() dashboardSnapshot {
	allGroups := GetAllGroups()
	snap := dashboardSnapshot{
		Type:   "snapshot",
		Groups: make([]groupSnapshot, 0, len(allGroups)),
	}

	for name, g := range allGroups {
		if !g.DashboardEnabled() {
			continue
		}
		entries := g.Entries()
		entrySnaps := make([]entrySnapshot, len(entries))
		for i, e := range entries {
			entrySnaps[i] = entrySnapshot(e)
		}

		snap.Groups = append(snap.Groups, groupSnapshot{
			Name:    name,
			Stats:   g.Stats(),
			Entries: entrySnaps,
		})
	}

	return snap
}

func handleCommand(cmd dashboardCommand) {
	g := GetGroup(cmd.Group)
	if g == nil || !g.DashboardEnabled() {
		log.Printf("[Dashboard] group %q not found or dashboard disabled", cmd.Group)
		return
	}

	ctx := context.Background()

	switch cmd.Action {
	case "delete":
		if err := g.Delete(ctx, cmd.Key); err != nil {
			log.Printf("[Dashboard] delete %q from %q failed: %v", cmd.Key, cmd.Group, err)
		}
	default:
		log.Printf("[Dashboard] unknown action %q", cmd.Action)
	}
}
