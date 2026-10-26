package react

// NopHooks 是 Hooks 的空实现。嵌入到你自己的 hooks 结构体中，
// 即可只覆写你关心的回调。
type NopHooks struct{}

func (NopHooks) OnAfterLLM(*AgentContext)        {}
func (NopHooks) OnBeforeTerminal(*AgentContext)  {}
func (NopHooks) OnBeforeInterrupt(*AgentContext) {}
func (NopHooks) OnBeforeRelay(*AgentContext)     {}
