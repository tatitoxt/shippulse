package main

import (
	"log"
	"net/http"
	"os"
	"sync"

	"github.com/gorilla/websocket"
	"github.com/nats-io/nats.go"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		return true // Allow UI connections
	},
}

type ClientHub struct {
	clients map[*websocket.Conn]bool
	mu      sync.Mutex
}

func main() {
	log.Println("Starting ShipPulse Realtime Gateway Service (WebSockets)...")

	port := os.Getenv("PORT")
	if port == "" {
		port = "8082"
	}
	natsURL := os.Getenv("NATS_URL")
	if natsURL == "" {
		natsURL = "nats://nats:4222"
	}

	hub := &ClientHub{
		clients: make(map[*websocket.Conn]bool),
	}

	// Connect NATS
	nc, err := nats.Connect(natsURL)
	if err != nil {
		log.Printf("Warning: Realtime gateway NATS connection failed (%v). Operating standalone...", err)
	} else {
		defer nc.Close()
		log.Println("Connected realtime gateway to NATS.")

		// Subscribe to all ShipPulse events and broadcast to WebSocket clients
		_, _ = nc.Subscribe("shippulse.events.>", func(m *nats.Msg) {
			hub.broadcast(m.Data)
		})
	}

	http.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"healthy","service":"shippulse-realtime"}`))
	})

	http.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			log.Printf("WebSocket upgrade error: %v", err)
			return
		}

		hub.mu.Lock()
		hub.clients[conn] = true
		hub.mu.Unlock()

		log.Printf("New WebSocket client connected. Active clients: %d", len(hub.clients))

		// Keep connection open until client disconnects
		for {
			_, _, err := conn.ReadMessage()
			if err != nil {
				hub.mu.Lock()
				delete(hub.clients, conn)
				hub.mu.Unlock()
				conn.Close()
				log.Println("WebSocket client disconnected.")
				break
			}
		}
	})

	log.Printf("Realtime WebSocket Gateway listening on port :%s", port)
	if err := http.ListenAndServe(":"+port, nil); err != nil {
		log.Fatalf("Realtime server shutdown: %v", err)
	}
}

func (h *ClientHub) broadcast(data []byte) {
	h.mu.Lock()
	defer h.mu.Unlock()

	for client := range h.clients {
		err := client.WriteMessage(websocket.TextMessage, data)
		if err != nil {
			client.Close()
			delete(h.clients, client)
		}
	}
}
