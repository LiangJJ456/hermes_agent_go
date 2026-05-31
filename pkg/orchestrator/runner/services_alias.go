package runner

import agcontext "code.byted.org/ad_creative/hermes_agent_go/pkg/orchestrator/context"

// These types moved to the context package (so they can live on ExecutionContext
// without an import cycle). Aliases keep existing runner.X references working.
type (
	LLMConfig            = agcontext.LLMConfig
	LLMMessage           = agcontext.LLMMessage
	LLMInvoker           = agcontext.LLMInvoker
	ToolInvoker          = agcontext.ToolInvoker
	GraphExecutor        = agcontext.GraphExecutor
	ContextGraphExecutor = agcontext.ContextGraphExecutor
)
