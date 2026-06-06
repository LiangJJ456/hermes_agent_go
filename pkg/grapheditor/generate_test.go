package grapheditor

import (
	"context"
	"errors"
	"testing"

	"code.byted.org/ad_creative/hermes_agent_go/pkg/types"
)

// scriptedChat returns the next canned reply per call.
func scriptedChat(replies ...string) (ChatFunc, *int) {
	n := 0
	calls := &n
	return func(_ context.Context, _ []types.Message) (string, error) {
		i := *calls
		*calls++
		if i >= len(replies) {
			return replies[len(replies)-1], nil
		}
		return replies[i], nil
	}, calls
}

const genValidGraph = `{"StartAt":"a","Nodes":{"a":{"Type":"end","Config":{}}},"Edges":[]}`
const genInvalidGraph = `{"StartAt":"","Nodes":{},"Edges":[]}` // StartAt empty -> invalid

func TestGenerate_FirstTrySuccess(t *testing.T) {
	chat, calls := scriptedChat(genValidGraph)
	g := NewGraphGenerator(chat, BuildNodeTypeSchemas(), 2)
	res, err := g.Generate(context.Background(), "make an end graph", nil)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Valid || res.Attempts != 1 || *calls != 1 {
		t.Fatalf("expected valid first try, got valid=%v attempts=%d calls=%d", res.Valid, res.Attempts, *calls)
	}
}

func TestGenerate_CorrectsAfterError(t *testing.T) {
	chat, _ := scriptedChat(genInvalidGraph, genValidGraph)
	g := NewGraphGenerator(chat, BuildNodeTypeSchemas(), 2)
	res, err := g.Generate(context.Background(), "fix it", nil)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Valid || res.Attempts != 2 {
		t.Fatalf("expected valid on 2nd attempt, got valid=%v attempts=%d", res.Valid, res.Attempts)
	}
}

func TestGenerate_ExhaustedReturnsLast(t *testing.T) {
	chat, _ := scriptedChat(genInvalidGraph, genInvalidGraph, genInvalidGraph)
	g := NewGraphGenerator(chat, BuildNodeTypeSchemas(), 2)
	res, err := g.Generate(context.Background(), "nope", nil)
	if err != nil {
		t.Fatal(err)
	}
	if res.Valid || res.Attempts != 3 || len(res.Errors) == 0 {
		t.Fatalf("expected invalid last version, got valid=%v attempts=%d errs=%d", res.Valid, res.Attempts, len(res.Errors))
	}
}

func TestGenerate_NoJSONErrors(t *testing.T) {
	chat, _ := scriptedChat("我无法生成", "还是不行")
	g := NewGraphGenerator(chat, BuildNodeTypeSchemas(), 1)
	_, err := g.Generate(context.Background(), "x", nil)
	if !errors.Is(err, ErrNoValidJSON) {
		t.Fatalf("expected ErrNoValidJSON, got %v", err)
	}
}
