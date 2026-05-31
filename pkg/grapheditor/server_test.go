package grapheditor

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	_ "code.byted.org/ad_creative/hermes_agent_go/pkg/orchestrator/runner"
)

func TestHandler_NodeTypes(t *testing.T) {
	srv := httptest.NewServer(NewHandler())
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
	srv := httptest.NewServer(NewHandler())
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
	srv := httptest.NewServer(NewHandler())
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
	srv := httptest.NewServer(NewHandler())
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
