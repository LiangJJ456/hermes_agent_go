package adapters

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"code.byted.org/ad_creative/hermes_agent_go/pkg/orchestrator"
)

// execFunc matches registry.Execute's signature so tests can inject a fake.
type execFunc func(ctx context.Context, resource string, args json.RawMessage) (string, error)

// runStandardTool runs a standard tool with soft-timeout-to-background.
//   - completes within `after` → return its Output normally.
//   - exceeds `after` → return a placeholder Output now; the tool keeps running
//     on bgCtx and calls `notify` with the result when it finishes.
//   - turn ctx cancelled first → cancel the tool, return ctx.Err().
//   - after <= 0 → background disabled: block until the tool returns.
func runStandardTool(
	ctx context.Context,
	bgCtx context.Context,
	after time.Duration,
	notify func(xml string),
	resource string,
	args json.RawMessage,
	exec execFunc,
) (*orchestrator.NodeResult, error) {
	if after <= 0 {
		out, err := exec(ctx, resource, args)
		if err != nil {
			return &orchestrator.NodeResult{Status: orchestrator.StatusContinue, Output: fmt.Sprintf("Error: %v", err)}, nil
		}
		return &orchestrator.NodeResult{Status: orchestrator.StatusContinue, Output: out}, nil
	}

	root := bgCtx
	if root == nil {
		root = context.Background()
	}
	toolCtx, toolCancel := context.WithCancel(root)

	type outcome struct {
		out string
		err error
	}
	done := make(chan outcome, 1)
	go func() {
		out, err := exec(toolCtx, resource, args)
		done <- outcome{out, err}
	}()

	select {
	case res := <-done:
		toolCancel()
		if res.err != nil {
			return &orchestrator.NodeResult{Status: orchestrator.StatusContinue, Output: fmt.Sprintf("Error: %v", res.err)}, nil
		}
		return &orchestrator.NodeResult{Status: orchestrator.StatusContinue, Output: res.out}, nil

	case <-time.After(after):
		go func() {
			res := <-done
			toolCancel() // tool finished in the background; release its ctx now
			if notify != nil {
				notify(buildToolNotification(resource, args, res.out, res.err))
			}
		}()
		placeholder := fmt.Sprintf("⏳ 工具「%s」已转入后台执行(超过 %s)。结果稍后将以系统通知送达,请继续,无需等待此结果。", resource, after)
		return &orchestrator.NodeResult{Status: orchestrator.StatusContinue, Output: placeholder}, nil

	case <-ctx.Done():
		toolCancel()
		return nil, ctx.Err()
	}
}

// buildToolNotification formats a finished background tool's result as the
// notification XML injected back into the conversation (parallels the async
// delegate notification style). On failure it emits an error attribute.
func buildToolNotification(resource string, args json.RawMessage, output string, execErr error) string {
	argsStr := string(args)
	if len(argsStr) > 200 {
		argsStr = argsStr[:200]
	}
	if execErr != nil {
		return fmt.Sprintf("<background_tool_result tool=%q args=%q>\n  <result error=%q/>\n</background_tool_result>",
			resource, argsStr, execErr.Error())
	}
	return fmt.Sprintf("<background_tool_result tool=%q args=%q>\n  <result>%s</result>\n</background_tool_result>",
		resource, argsStr, output)
}
