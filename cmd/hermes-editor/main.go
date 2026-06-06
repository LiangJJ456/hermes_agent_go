package main

import (
	"context"
	"flag"
	"log"
	"net/http"

	"code.byted.org/ad_creative/hermes_agent_go/pkg/grapheditor"
	"code.byted.org/ad_creative/hermes_agent_go/pkg/model"
	"code.byted.org/ad_creative/hermes_agent_go/pkg/model/setup"
	"code.byted.org/ad_creative/hermes_agent_go/pkg/types"
	// Blank import registers all node types (llm/tool/choice/parallel/human/end)
	// via their init() funcs, so ListNodeTypes/schema export sees them.
	_ "code.byted.org/ad_creative/hermes_agent_go/pkg/orchestrator/runner"
)

func main() {
	addr := flag.String("addr", "127.0.0.1:7390", "address to listen on (host:port)")
	flag.Parse()

	cfg := types.LoadConfig()
	router, _, modelSpec := setup.BuildRouter(&cfg)

	var gen *grapheditor.GraphGenerator
	if provider, modelName, err := router.Resolve(modelSpec); err == nil {
		chat := func(ctx context.Context, messages []types.Message) (string, error) {
			resp, err := provider.Chat(ctx, &model.ChatRequest{
				Model:       modelName,
				Messages:    messages,
				Temperature: 0.2,
			})
			if err != nil {
				return "", err
			}
			return resp.Message.Content, nil
		}
		gen = grapheditor.NewGraphGenerator(chat, grapheditor.BuildNodeTypeSchemas(), 2)
		log.Printf("AI generation enabled (model: %s)", modelSpec)
	} else {
		log.Printf("AI generation disabled: no model configured (%v)", err)
	}

	log.Printf("hermes graph editor listening on http://%s", *addr)
	if err := http.ListenAndServe(*addr, grapheditor.NewHandler(gen)); err != nil {
		log.Fatalf("server error: %v", err)
	}
}
