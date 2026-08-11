package main

import (
	"encoding/json"
	"log"
	"net/http"
	"os"
	"time"
)

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "9090"
	}

	http.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"status":    "healthy",
			"service":   "demo-api",
			"version":   "v1.0.0",
			"timestamp": time.Now().UTC(),
		})
	})

	http.HandleFunc("/api/checkout", func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(20 * time.Millisecond) // Simulate work
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "success", "order_id": "ord-8812"})
	})

	log.Printf("Demo Microservice API listening on :%s", port)
	if err := http.ListenAndServe(":"+port, nil); err != nil {
		log.Fatalf("Demo API error: %v", err)
	}
}
