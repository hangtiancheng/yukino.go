package appkitfake

import "github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/core/react"

// FakeHooks 是 Phase 1 用的空 hooks 实现，后续可扩展。
type FakeHooks struct {
	react.NopHooks
}
