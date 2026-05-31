package grapheditor

import (
	"fmt"

	"code.byted.org/ad_creative/hermes_agent_go/pkg/orchestrator"
)

// ValidationError is one problem found in a graph, with a best-effort path.
type ValidationError struct {
	Path    string `json:"path"`
	Message string `json:"message"`
}

// ValidateResponse is the result of validating a graph JSON document.
type ValidateResponse struct {
	Valid  bool              `json:"valid"`
	Errors []ValidationError `json:"errors"`
}

// ValidateGraph parses and structurally checks a graph JSON document.
// Parse-level failures (structure, unknown node types, bad config, bad edge
// conditions) surface from UnmarshalGraph as a single error with path
// "<graph>". If parsing succeeds, structural checks run: StartAt must name an
// existing node, and every edge's non-empty From/To must name existing nodes.
//
// Known limitations (not yet validated): a blank edge From/To is accepted;
// node-name references in Graph.OnError/OnTimeout, NodeSpec.Catch[].Next, and
// choice-node ChoiceEntry.Next/Default are not checked against the node set.
func ValidateGraph(body []byte) ValidateResponse {
	g, err := orchestrator.UnmarshalGraph(body)
	if err != nil {
		return ValidateResponse{
			Valid:  false,
			Errors: []ValidationError{{Path: "<graph>", Message: err.Error()}},
		}
	}

	// non-nil so a valid graph serializes "errors":[] (not null) for the client
	errs := []ValidationError{}

	if g.StartAt == "" {
		errs = append(errs, ValidationError{Path: "StartAt", Message: "StartAt is empty"})
	} else if _, ok := g.Nodes[g.StartAt]; !ok {
		errs = append(errs, ValidationError{
			Path:    "StartAt",
			Message: fmt.Sprintf("StartAt references unknown node %q", g.StartAt),
		})
	}

	for i, e := range g.Edges {
		if e.From != "" {
			if _, ok := g.Nodes[e.From]; !ok {
				errs = append(errs, ValidationError{
					Path:    fmt.Sprintf("edges[%d].from", i),
					Message: fmt.Sprintf("edge %d: From references unknown node %q", i, e.From),
				})
			}
		}
		if e.To != "" {
			if _, ok := g.Nodes[e.To]; !ok {
				errs = append(errs, ValidationError{
					Path:    fmt.Sprintf("edges[%d].to", i),
					Message: fmt.Sprintf("edge %d: To references unknown node %q", i, e.To),
				})
			}
		}
	}

	return ValidateResponse{Valid: len(errs) == 0, Errors: errs}
}
