package grapheditor

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	_ "code.byted.org/ad_creative/hermes_agent_go/pkg/orchestrator/runner"
	"code.byted.org/ad_creative/hermes_agent_go/pkg/types"
)

func TestHandler_NodeTypes(t *testing.T) {
	srv := httptest.NewServer(NewHandler(nil))
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/nodetypes")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	var schemas []NodeTypeSchema
	if err := json.NewDecoder(resp.Body).Decode(&schemas); err != nil {
		t.Fatal(err)
	}
	if findSchema(schemas, "llm") == nil {
		t.Fatalf("expected llm in schemas, got %d types", len(schemas))
	}
}

func TestHandler_ValidateOK(t *testing.T) {
	srv := httptest.NewServer(NewHandler(nil))
	defer srv.Close()

	resp, err := http.Post(srv.URL+"/api/validate", "application/json",
		strings.NewReader(validGraph))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	var vr ValidateResponse
	if err := json.NewDecoder(resp.Body).Decode(&vr); err != nil {
		t.Fatal(err)
	}
	if !vr.Valid {
		t.Fatalf("expected valid, got %+v", vr.Errors)
	}
}

func TestHandler_ValidateNonJSON(t *testing.T) {
	srv := httptest.NewServer(NewHandler(nil))
	defer srv.Close()

	resp, err := http.Post(srv.URL+"/api/validate", "text/plain",
		strings.NewReader("this is not json"))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
}

func TestHandler_ServesIndex(t *testing.T) {
	srv := httptest.NewServer(NewHandler(nil))
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "Hermes Graph Editor") {
		t.Fatalf("index page not served, body: %q", string(body))
	}
}

func TestHandler_MethodNotAllowed(t *testing.T) {
	srv := httptest.NewServer(NewHandler(nil))
	defer srv.Close()

	// /api/nodetypes only allows GET
	resp, err := http.Post(srv.URL+"/api/nodetypes", "application/json",
		strings.NewReader("{}"))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("POST /api/nodetypes status = %d, want 405", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "application/json" {
		t.Fatalf("405 Content-Type = %q, want application/json", ct)
	}
}

func TestGenerate_NotConfigured(t *testing.T) {
	srv := httptest.NewServer(NewHandler(nil))
	defer srv.Close()
	resp, err := http.Post(srv.URL+"/api/generate", "application/json",
		bytes.NewReader([]byte(`{"instruction":"x"}`)))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("want 503, got %d", resp.StatusCode)
	}
}

func TestGenerate_Success(t *testing.T) {
	chat := func(_ context.Context, _ []types.Message) (string, error) {
		return `{"StartAt":"a","Nodes":{"a":{"Type":"end","Config":{}}},"Edges":[]}`, nil
	}
	gen := NewGraphGenerator(chat, BuildNodeTypeSchemas(), 1)
	srv := httptest.NewServer(NewHandler(gen))
	defer srv.Close()
	resp, err := http.Post(srv.URL+"/api/generate", "application/json",
		bytes.NewReader([]byte(`{"instruction":"make end"}`)))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("want 200, got %d", resp.StatusCode)
	}
	var res GenerateResult
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		t.Fatal(err)
	}
	if !res.Valid {
		t.Fatalf("expected valid result")
	}
}
