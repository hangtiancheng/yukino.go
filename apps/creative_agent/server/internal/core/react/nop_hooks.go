package react

// NopHooks is the no-op implementation of Hooks. Embed it in your own hooks struct
// to override only the callbacks you care about.
type NopHooks struct{}

func (NopHooks) OnAfterLLM(*AgentContext)        {}
func (NopHooks) OnBeforeTerminal(*AgentContext)  {}
func (NopHooks) OnBeforeInterrupt(*AgentContext) {}
func (NopHooks) OnBeforeRelay(*AgentContext)     {}
