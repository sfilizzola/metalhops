package main

import (
	"encoding/json"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/metalhops/metalhops/backend"
)

func newMux() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", healthHandler)
	mux.HandleFunc("GET /openapi.yaml", openAPIHandler)
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

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	srv := &http.Server{
		Addr:              ":" + port,
		Handler:           newMux(),
		ReadHeaderTimeout: 5 * time.Second,
	}

	log.Printf("MetalHops API listening on :%s", port)
	log.Fatal(srv.ListenAndServe())
}
