package main

import (
	"flag"
	"log"
	"net/http"

	"code.byted.org/ad_creative/hermes_agent_go/pkg/grapheditor"
	// Blank import registers all node types (llm/tool/choice/parallel/human/end)
	// via their init() funcs, so ListNodeTypes/schema export sees them.
	_ "code.byted.org/ad_creative/hermes_agent_go/pkg/orchestrator/runner"
)

func main() {
	addr := flag.String("addr", "127.0.0.1:7390", "address to listen on (host:port)")
	flag.Parse()

	log.Printf("hermes graph editor listening on http://%s", *addr)
	if err := http.ListenAndServe(*addr, grapheditor.NewHandler()); err != nil {
		log.Fatalf("server error: %v", err)
	}
}
