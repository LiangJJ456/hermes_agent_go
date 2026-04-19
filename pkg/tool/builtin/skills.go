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
	skillsToolName  = "skills"
	skillFileName   = "SKILL.md"
	maxActiveSkills = 5
)

// SkillManager manages progressive skill discovery and activation.
type SkillManager struct {
	mu          sync.RWMutex
	discovered  map[string]*SkillInfo
	active      []string
	searchPaths []string
}

// SkillInfo holds metadata for a discovered skill.
type SkillInfo struct {
	Name        string `json:"name"`
	Path        string `json:"path"`
	Description string `json:"description,omitempty"`
	Active      bool   `json:"active"`
	Content     string `json:"-"`
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

			desc := extractDescription(skillPath)
			sm.discovered[name] = &SkillInfo{
				Name:        name,
				Path:        skillPath,
				Description: desc,
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

	lines := strings.Split(string(data), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if len(line) > 120 {
			line = line[:120] + "..."
		}
		return line
	}
	return ""
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
				Description: "Discover and activate skills (reusable prompt-driven capabilities). " +
					"Skills are loaded from SKILL.md files in the skills directories.\n\n" +
					"ACTIONS:\n" +
					"- list: Show all discovered skills and their activation status\n" +
					"- activate: Load a skill's full instructions (max 5 active)\n" +
					"- deactivate: Unload a skill to free a slot\n" +
					"- read: Get the full content of an active skill",
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
							"description": "Skill name (required for activate/deactivate/read).",
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

func (sm *SkillManager) handleSkillsTool(_ context.Context, raw json.RawMessage) (string, error) {
	var args skillsArgs
	if err := json.Unmarshal(raw, &args); err != nil {
		return toolErr("invalid arguments: %v", err), nil
	}

	switch args.Action {
	case "list":
		return sm.listSkills(), nil
	case "activate":
		if args.Name == "" {
			return toolErr("name is required for activate"), nil
		}
		return sm.activateSkill(args.Name), nil
	case "deactivate":
		if args.Name == "" {
			return toolErr("name is required for deactivate"), nil
		}
		return sm.deactivateSkill(args.Name), nil
	case "read":
		if args.Name == "" {
			return toolErr("name is required for read"), nil
		}
		return sm.readSkill(args.Name), nil
	default:
		return toolErr("Unknown action '%s'. Use: list, activate, deactivate, read", args.Action), nil
	}
}

func (sm *SkillManager) listSkills() string {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	type skillSummary struct {
		Name        string `json:"name"`
		Description string `json:"description,omitempty"`
		Active      bool   `json:"active"`
	}

	var skills []skillSummary
	for _, info := range sm.discovered {
		skills = append(skills, skillSummary{
			Name:        info.Name,
			Description: info.Description,
			Active:      info.Active,
		})
	}

	result := map[string]any{
		"skills":     skills,
		"total":      len(skills),
		"active":     len(sm.active),
		"max_active": maxActiveSkills,
		"slots_free": maxActiveSkills - len(sm.active),
	}

	b, _ := json.MarshalIndent(result, "", "  ")
	return string(b)
}

func (sm *SkillManager) activateSkill(name string) string {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	info, exists := sm.discovered[name]
	if !exists {
		return toolErr("Skill '%s' not found. Use action='list' to see available skills.", name)
	}

	if info.Active {
		return fmt.Sprintf("Skill '%s' is already active.", name)
	}

	if len(sm.active) >= maxActiveSkills {
		return toolErr("Maximum %d active skills reached. Deactivate one first. Active: %v",
			maxActiveSkills, sm.active)
	}

	data, err := os.ReadFile(info.Path)
	if err != nil {
		return toolErr("Failed to load skill '%s': %v", name, err)
	}

	info.Content = string(data)
	info.Active = true
	sm.active = append(sm.active, name)

	log.Info("skills: activated", "name", name, "content_len", len(info.Content))

	return fmt.Sprintf("Skill '%s' activated (%d/%d slots used). Use action='read' to view its instructions.",
		name, len(sm.active), maxActiveSkills)
}

func (sm *SkillManager) deactivateSkill(name string) string {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	info, exists := sm.discovered[name]
	if !exists {
		return toolErr("Skill '%s' not found.", name)
	}

	if !info.Active {
		return fmt.Sprintf("Skill '%s' is not active.", name)
	}

	info.Active = false
	info.Content = ""

	for i, n := range sm.active {
		if n == name {
			sm.active = append(sm.active[:i], sm.active[i+1:]...)
			break
		}
	}

	log.Info("skills: deactivated", "name", name)

	return fmt.Sprintf("Skill '%s' deactivated (%d/%d slots used).",
		name, len(sm.active), maxActiveSkills)
}

func (sm *SkillManager) readSkill(name string) string {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	info, exists := sm.discovered[name]
	if !exists {
		return toolErr("Skill '%s' not found.", name)
	}

	if !info.Active {
		return toolErr("Skill '%s' is not active. Use action='activate' first.", name)
	}

	return info.Content
}

// GetActiveSkillContents returns all active skill contents for system prompt injection.
func (sm *SkillManager) GetActiveSkillContents() map[string]string {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	result := make(map[string]string)
	for _, name := range sm.active {
		if info, ok := sm.discovered[name]; ok && info.Active && info.Content != "" {
			result[name] = info.Content
		}
	}
	return result
}
