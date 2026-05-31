package grapheditor

import (
	"embed"
	"encoding/json"
	"io"
	"io/fs"
	"log"
	"net/http"
)

//go:embed static
var staticFS embed.FS

// NewHandler builds the editor HTTP handler: the two JSON APIs plus the
// embedded static frontend served at the root.
func NewHandler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/nodetypes", handleNodeTypes)
	mux.HandleFunc("/api/validate", handleValidate)

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
