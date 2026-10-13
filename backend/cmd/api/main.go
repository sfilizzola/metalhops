package main

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/metalhops/metalhops/backend"
)

//go:embed docs.html
var docsHTML []byte

const docsCSP = "default-src 'none'; script-src https://cdn.jsdelivr.net; style-src 'unsafe-inline'; font-src https://fonts.scalar.com; img-src 'self' data:; connect-src 'self'; base-uri 'none'; form-action 'self'; frame-ancestors 'none'"

func docsUIEnabled(value string) (bool, error) {
	switch value {
	case "", "true":
		return true, nil
	case "false":
		return false, nil
	default:
		return false, fmt.Errorf("DOCS_UI_ENABLED must be true, false, or empty")
	}
}

func newMux(docsEnabled bool) *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", healthHandler)
	mux.HandleFunc("GET /openapi.yaml", openAPIHandler)
	if docsEnabled {
		mux.HandleFunc("GET /docs", docsHandler)
	}
	return mux
}

func healthHandler(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func openAPIHandler(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/yaml")
	_, _ = w.Write(contract.OpenAPI)
}

func docsHandler(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Content-Security-Policy", docsCSP)
	_, _ = w.Write(docsHTML)
}

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	docsEnabled, err := docsUIEnabled(os.Getenv("DOCS_UI_ENABLED"))
	if err != nil {
		log.Fatal(err)
	}

	srv := &http.Server{
		Addr:              ":" + port,
		Handler:           newMux(docsEnabled),
		ReadHeaderTimeout: 5 * time.Second,
	}

	log.Printf("MetalHops API listening on :%s", port)
	log.Fatal(srv.ListenAndServe())
}
