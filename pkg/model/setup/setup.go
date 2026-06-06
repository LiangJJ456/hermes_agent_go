// Package setup builds a model.Router from AgentConfig. Extracted from
// cmd/hermes so both hermes and hermes-editor share identical model wiring.
package setup

import (
	"fmt"
	"os"
	"strings"

	"code.byted.org/ad_creative/hermes_agent_go/pkg/model"
	"code.byted.org/ad_creative/hermes_agent_go/pkg/model/deepseek"
	"code.byted.org/ad_creative/hermes_agent_go/pkg/model/mimo"
	"code.byted.org/ad_creative/hermes_agent_go/pkg/model/openai"
	"code.byted.org/ad_creative/hermes_agent_go/pkg/types"
)

// BuildRouter assembles a model.Router from cfg: the main provider (resolved
// from cfg.Model + cfg.APIKey), any custom_providers, and an auto-switch to the
// first registered provider when cfg.Model is unresolvable. It mutates cfg.Model
// on auto-switch and returns the router, the main provider (may be nil when no
// API key is configured), and the final model spec.
func BuildRouter(cfg *types.AgentConfig) (*model.Router, model.Provider, string) {
	router := model.NewRouter()
	var mainProvider model.Provider

	if cfg.Model != "" && cfg.APIKey != "" {
		providerName, _, _ := strings.Cut(cfg.Model, "/")
		switch providerName {
		case "deepseek":
			mainProvider = deepseek.New(cfg.APIKey, cfg.BaseURL)
		default:
			mainProvider = openai.New(cfg.APIKey, cfg.BaseURL)
		}
		router.Register(mainProvider)
	}

	for _, cp := range cfg.CustomProviders {
		if cp.Name == "" || cp.APIKey == "" {
			continue
		}
		maxCtx := 128000
		if cp.Model != "" {
			if mc, ok := cp.Models[cp.Model]; ok && mc.ContextLength > 0 {
				maxCtx = mc.ContextLength
			}
		}
		var p model.Provider
		switch {
		case strings.Contains(cp.Name, "xiaomi"), strings.Contains(cp.Name, "mimo"),
			strings.Contains(cp.BaseURL, "xiaomimimo"):
			p = mimo.NewWithName(cp.Name, cp.APIKey, cp.BaseURL, maxCtx)
		default:
			p = openai.NewWithName(cp.Name, cp.APIKey, cp.BaseURL, maxCtx)
		}
		router.Register(p)
		fmt.Fprintf(os.Stderr, "Custom provider registered: %s (%s)\n", cp.Name, cp.BaseURL)
	}

	if _, _, err := router.Resolve(cfg.Model); err != nil {
		if first := router.FirstRegistered(); first != nil {
			oldModel := cfg.Model
			for _, cp := range cfg.CustomProviders {
				if cp.Name == first.Name() && cp.Model != "" {
					cfg.Model = cp.Name + "/" + cp.Model
					break
				}
			}
			if cfg.Model == oldModel {
				cfg.Model = first.Name() + "/" + oldModel
			}
			fmt.Fprintf(os.Stderr, "Model %q unavailable, auto-switched to %q\n", oldModel, cfg.Model)
		}
	}

	return router, mainProvider, cfg.Model
}
