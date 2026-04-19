// Package builtin implements the core built-in tools for hermes-agent.
//
// Import this package for side-effect registration:
//
//	import _ "code.byted.org/ad_creative/hermes_agent_go/pkg/tool/builtin"
//
// This registers: bash, read_file, write_file, edit_file, list_dir, search_files.
// The skills tool requires explicit initialization via NewSkillManager().RegisterTool().
package builtin
