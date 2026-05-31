package executor

import (
	"context"
	"io"

	"code.byted.org/ad_creative/hermes_agent_go/pkg/orchestrator"
	"code.byted.org/ad_creative/hermes_agent_go/pkg/orchestrator/schema"
)

// ExecuteStream executes a graph and returns a StreamReader for streaming output.
func (e *Executor) ExecuteStream(ctx context.Context, g *orchestrator.Graph,
	input interface{}) (*schema.StreamReader, error) {
	sw, sr := schema.Pipe(8)
	go func() {
		defer sw.Close()
		output, _, err := e.Execute(ctx, g, input)
		if err != nil {
			sw.Send(nil, err)
			return
		}
		sw.Send(output, nil)
	}()
	return sr, nil
}

// ConcatStreamReader reads all frames from a StreamReader and concatenates them.
func ConcatStreamReader(sr *schema.StreamReader) (interface{}, error) {
	var chunks []interface{}
	for {
		chunk, err := sr.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		chunks = append(chunks, chunk)
	}
	if len(chunks) == 0 {
		return nil, nil
	}
	if len(chunks) == 1 {
		return chunks[0], nil
	}
	var result string
	for _, c := range chunks {
		if s, ok := c.(string); ok {
			result += s
		} else {
			return chunks, nil
		}
	}
	return result, nil
}

// BoxValue wraps a single value as a StreamReader.
func BoxValue(v interface{}) *schema.StreamReader {
	return schema.StreamReaderFromArray([]interface{}{v})
}
