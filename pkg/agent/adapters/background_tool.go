package adapters

import (
	"encoding/json"
	"fmt"
)

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
