package appkitfake

import "github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/core/react"

// FakeHooks is the no-op hooks implementation for Phase 1; extensible later.
type FakeHooks struct {
	react.NopHooks
}
