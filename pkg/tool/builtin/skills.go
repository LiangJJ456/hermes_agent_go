package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"code.byted.org/ad_creative/hermes_agent_go/pkg/log"
	"code.byted.org/ad_creative/hermes_agent_go/pkg/tool/registry"
	"code.byted.org/ad_creative/hermes_agent_go/pkg/types"
)

const (
	skillsToolName = "skills"
	skillFileName  = "SKILL.md"
)

// SkillManager 负责 skill 的发现与内容读取（无激活状态）。
// 激活状态是 per-agent 的，由各 agent 自己持有，SkillManager 仅作为发现/读取后端，
// 因此父子 agent 可共享同一个 SkillManager 实例而互不干扰。
type SkillManager struct {
	mu          sync.RWMutex
	discovered  map[string]*SkillInfo
	searchPaths []string
}

// SkillInfo holds metadata for a discovered skill.
type SkillInfo struct {
	Name        string `json:"name"`
	Path        string `json:"path"`
	Description string `json:"description,omitempty"`
}

// NewSkillManager creates a skill manager and scans for skills.
func NewSkillManager(searchPaths []string) *SkillManager {
	sm := &SkillManager{
		discovered:  make(map[string]*SkillInfo),
		searchPaths: searchPaths,
	}
	sm.scan()
	return sm
}

func (sm *SkillManager) scan() {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	for _, base := range sm.searchPaths {
		entries, err := os.ReadDir(base)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			skillPath := filepath.Join(base, e.Name(), skillFileName)
			if _, err := os.Stat(skillPath); err != nil {
				continue
			}

			name := e.Name()
			if _, exists := sm.discovered[name]; exists {
				continue
			}

			sm.discovered[name] = &SkillInfo{
				Name:        name,
				Path:        skillPath,
				Description: extractDescription(skillPath),
			}
		}
	}

	log.Info("skills: scan complete", "discovered", len(sm.discovered))
}

func extractDescription(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if len(line) > 120 {
			return line[:120] + "..."
		}
		return line
	}
	return ""
}

// Lookup 按名称返回已发现的 skill 信息。
func (sm *SkillManager) Lookup(name string) (*SkillInfo, bool) {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	info, ok := sm.discovered[name]
	return info, ok
}

// ReadContent 读取某个已发现 skill 的完整 SKILL.md 内容。
func (sm *SkillManager) ReadContent(name string) (string, error) {
	sm.mu.RLock()
	info, ok := sm.discovered[name]
	sm.mu.RUnlock()
	if !ok {
		return "", fmt.Errorf("skill '%s' not found", name)
	}
	data, err := os.ReadFile(info.Path)
	if err != nil {
		return "", err
	}
	log.Info("skills: read", "name", name, "bytes", len(data))
	return string(data), nil
}

// ActiveSection 根据给定的激活集合构建描述块（名称+描述），无激活时返回 ""。
// 激活集合由调用方（agent）持有，SkillManager 只负责按发现的元数据渲染。
func (sm *SkillManager) ActiveSection(active map[string]bool) string {
	if len(active) == 0 {
		return ""
	}
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	var infos []*SkillInfo
	for name := range active {
		if info, ok := sm.discovered[name]; ok {
			infos = append(infos, info)
		}
	}
	if len(infos) == 0 {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("## Active Skills\n\n")
	for _, info := range infos {
		sb.WriteString(fmt.Sprintf("- **%s**: %s\n", info.Name, info.Description))
	}
	sb.WriteString("\nUse `skills(action=read, name=<skill>)` to load a skill's full instructions.")
	return sb.String()
}

// RegisterTool registers the skills tool in the global registry.
func (sm *SkillManager) RegisterTool() {
	registry.Global().Register(&registry.ToolEntry{
		Name:    skillsToolName,
		Toolset: toolsetCore,
		Schema: types.ToolSchema{
			Type: "function",
			Function: types.FunctionSchema{
				Name: skillsToolName,
				Description: "Discover and use skills (reusable prompt-driven capabilities loaded from SKILL.md files).\n\n" +
					"ACTIONS:\n" +
					"- list: Show all discovered skills and their descriptions\n" +
					"- activate: Activate a skill — injects its description into context so you stay aware of it\n" +
					"- deactivate: Deactivate a skill when it is no longer needed\n" +
					"- read: Load the full instructions for an active skill",
				Parameters: mustJSON(map[string]any{
					"type": "object",
					"properties": map[string]any{
						"action": map[string]any{
							"type":        "string",
							"enum":        []string{"list", "activate", "deactivate", "read"},
							"description": "The action to perform.",
						},
						"name": map[string]any{
							"type":        "string",
							"description": "Skill name (required for activate, deactivate, read).",
						},
					},
					"required": []string{"action"},
				}),
			},
		},
		Handler:      sm.handleSkillsTool,
		ParallelSafe: true,
	})
}

type skillsArgs struct {
	Action string `json:"action"`
	Name   string `json:"name,omitempty"`
}

// handleSkillsTool 是注册到 registry 的 fallback handler，仅用于暴露 schema。
// 实际执行（activate/deactivate/read 的 per-agent 状态）由 agent 在 executeToolCalls
// 中拦截处理；此 handler 只在未挂载到 agent 的退化场景被调用，支持无状态的 list。
func (sm *SkillManager) handleSkillsTool(_ context.Context, raw json.RawMessage) (string, error) {
	var args skillsArgs
	if err := json.Unmarshal(raw, &args); err != nil {
		return toolErr("invalid arguments: %v", err), nil
	}
	if args.Action == "list" {
		return sm.ListJSON(), nil
	}
	return toolErr("skills action '%s' requires an agent context", args.Action), nil
}

// ListJSON 返回所有已发现 skill 的名称+描述（JSON 字符串）。
func (sm *SkillManager) ListJSON() string {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	type entry struct {
		Name        string `json:"name"`
		Description string `json:"description,omitempty"`
	}

	skills := make([]entry, 0, len(sm.discovered))
	for _, info := range sm.discovered {
		skills = append(skills, entry{Name: info.Name, Description: info.Description})
	}

	b, _ := json.MarshalIndent(map[string]any{
		"skills": skills,
		"total":  len(skills),
	}, "", "  ")
	return string(b)
}
