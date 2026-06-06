package grapheditor

import (
	"embed"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"log"
	"net/http"
)

//go:embed static
var staticFS embed.FS

// NewHandler builds the editor HTTP handler: the JSON APIs plus the embedded
// static frontend. gen may be nil — then /api/generate returns 503.
func NewHandler(gen *GraphGenerator) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/nodetypes", handleNodeTypes)
	mux.HandleFunc("/api/validate", handleValidate)
	mux.HandleFunc("/api/generate", makeGenerateHandler(gen))

	sub, err := fs.Sub(staticFS, "static")
	if err != nil {
		panic(err) // embedded dir is known at build time
	}
	mux.Handle("/", http.FileServer(http.FS(sub)))
	return mux
}

func handleNodeTypes(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	writeJSON(w, http.StatusOK, BuildNodeTypeSchemas())
}

func handleValidate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if !json.Valid(body) {
		writeJSON(w, http.StatusBadRequest,
			map[string]string{"error": "request body is not valid JSON"})
		return
	}
	writeJSON(w, http.StatusOK, ValidateGraph(body))
}

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Printf("grapheditor: encode response: %v", err)
	}
}

type generateRequest struct {
	Instruction string          `json:"instruction"`
	Graph       json.RawMessage `json:"graph"`
}

func makeGenerateHandler(gen *GraphGenerator) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
			return
		}
		if gen == nil {
			writeJSON(w, http.StatusServiceUnavailable,
				map[string]string{"error": "no model configured (set OPENAI_API_KEY / HERMES_MODEL)"})
			return
		}
		var req generateRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		res, err := gen.Generate(r.Context(), req.Instruction, req.Graph)
		if err != nil {
			if errors.Is(err, ErrNoValidJSON) {
				writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": err.Error()})
				return
			}
			writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, res)
	}
}
