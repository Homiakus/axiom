package main

import (
	"log"
	"net/http"
	"os"
	"time"
)

func main() {
	defaultPath := "/mnt/data/hydropilot.ideal.rules.axm"
	if len(os.Args) > 1 {
		defaultPath = os.Args[1]
	}
	if b, err := os.ReadFile(defaultPath); err == nil {
		state.source = string(b)
		state.path = defaultPath
	} else {
		state.source = sampleSource
		state.path = ""
		state.msg = "Loaded built-in sample. Use Load or Upload to open your .axm file."
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/", withSecurityHeaders(handleIndex))
	mux.HandleFunc("/load", withSecurityHeaders(handleLoad))
	mux.HandleFunc("/upload", withSecurityHeaders(handleUpload))
	mux.HandleFunc("/update", withSecurityHeaders(handleUpdate))
	mux.HandleFunc("/save", withSecurityHeaders(handleSave))
	mux.HandleFunc("/stubs", withSecurityHeaders(handleStubs))
	mux.HandleFunc("/report", withSecurityHeaders(handleReport))
	mux.HandleFunc("/download-source", withSecurityHeaders(handleDownloadSource))
	mux.HandleFunc("/zip", withSecurityHeaders(handleZip))
	mux.HandleFunc("/healthz", withSecurityHeaders(handleHealth))

	addr := os.Getenv("AXIOM_STUDIO_ADDR")
	if addr == "" {
		addr = "127.0.0.1:8080"
	}
	srv := &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       120 * time.Second,
	}
	log.Printf("Axiom Rule Studio: http://%s", addr)
	if err := srv.ListenAndServe(); err != nil {
		log.Fatal(err)
	}
}
