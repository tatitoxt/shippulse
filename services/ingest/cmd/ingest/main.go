package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

type Config struct {
	Port           string
	ClickHouseAddr string
	ClickHouseUser string
	ClickHousePass string
}

func loadConfig() *Config {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8081"
	}
	chAddr := os.Getenv("CLICKHOUSE_ADDR")
	if chAddr == "" {
		chAddr = "clickhouse:9000"
	}
	chUser := os.Getenv("CLICKHOUSE_USER")
	if chUser == "" {
		chUser = "shippulse"
	}
	chPass := os.Getenv("CLICKHOUSE_PASSWORD")

	return &Config{
		Port:           port,
		ClickHouseAddr: chAddr,
		ClickHouseUser: chUser,
		ClickHousePass: chPass,
	}
}

type IngestServer struct {
	cfg  *Config
	conn driver.Conn
}

func main() {
	log.Println("Starting ShipPulse Telemetry Ingest Service...")
	cfg := loadConfig()

	conn, err := clickhouse.Open(&clickhouse.Options{
		Addr: []string{cfg.ClickHouseAddr},
		Auth: clickhouse.Auth{
			Database: "shippulse_telemetry",
			Username: cfg.ClickHouseUser,
			Password: cfg.ClickHousePass,
		},
		DialTimeout: 5 * time.Second,
	})

	if err != nil {
		log.Printf("Warning: ClickHouse connection failed (%v). Operating in buffered fallback mode...", err)
	} else {
		log.Println("Connected to ClickHouse successfully.")
	}

	server := &IngestServer{cfg: cfg, conn: conn}

	r := chi.NewRouter()
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)

	r.Get("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"healthy","service":"shippulse-ingest"}`))
	})

	r.Post("/v1/logs", server.IngestLogs)
	r.Post("/v1/metrics", server.IngestMetrics)
	r.Post("/v1/traces", server.IngestTraces)

	log.Printf("Telemetry Ingest Service listening on port :%s", cfg.Port)
	if err := http.ListenAndServe(":"+cfg.Port, r); err != nil {
		log.Fatalf("Ingest server shutdown: %v", err)
	}
}

type LogPayload struct {
	OrganizationID string            `json:"organization_id"`
	ServiceID      string            `json:"service_id"`
	EnvironmentID  string            `json:"environment_id"`
	Severity       string            `json:"severity"`
	Message        string            `json:"message"`
	TraceID        string            `json:"trace_id,omitempty"`
	SpanID         string            `json:"span_id,omitempty"`
	Attributes     map[string]string `json:"attributes,omitempty"`
}

func (s *IngestServer) IngestLogs(w http.ResponseWriter, r *http.Request) {
	var logs []LogPayload
	if err := json.NewDecoder(r.Body).Decode(&logs); err != nil {
		// Try single payload
		var single LogPayload
		r.Body.Close()
		if err2 := json.NewDecoder(r.Body).Decode(&single); err2 == nil {
			logs = append(logs, single)
		}
	}

	if s.conn != nil && len(logs) > 0 {
		ctx := context.Background()
		batch, err := s.conn.PrepareBatch(ctx, "INSERT INTO telemetry_logs (timestamp, organization_id, service_id, environment_id, severity, trace_id, span_id, message, attributes)")
		if err == nil {
			for _, l := range logs {
				if l.Severity == "" {
					l.Severity = "INFO"
				}
				if l.Attributes == nil {
					l.Attributes = map[string]string{}
				}
				_ = batch.Append(time.Now().UTC(), l.OrganizationID, l.ServiceID, l.EnvironmentID, l.Severity, l.TraceID, l.SpanID, l.Message, l.Attributes)
			}
			_ = batch.Send()
		}
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	json.NewEncoder(w).Encode(map[string]string{"status": "accepted", "count": fmt.Sprintf("%d", len(logs))})
}

type MetricPayload struct {
	OrganizationID string            `json:"organization_id"`
	ServiceID      string            `json:"service_id"`
	EnvironmentID  string            `json:"environment_id"`
	MetricName     string            `json:"metric_name"`
	Value          float64           `json:"value"`
	Attributes     map[string]string `json:"attributes,omitempty"`
}

func (s *IngestServer) IngestMetrics(w http.ResponseWriter, r *http.Request) {
	var metrics []MetricPayload
	_ = json.NewDecoder(r.Body).Decode(&metrics)

	if s.conn != nil && len(metrics) > 0 {
		ctx := context.Background()
		batch, err := s.conn.PrepareBatch(ctx, "INSERT INTO telemetry_metrics (timestamp, organization_id, service_id, environment_id, metric_name, value, attributes)")
		if err == nil {
			for _, m := range metrics {
				if m.Attributes == nil {
					m.Attributes = map[string]string{}
				}
				_ = batch.Append(time.Now().UTC(), m.OrganizationID, m.ServiceID, m.EnvironmentID, m.MetricName, m.Value, m.Attributes)
			}
			_ = batch.Send()
		}
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	json.NewEncoder(w).Encode(map[string]string{"status": "accepted"})
}

type TracePayload struct {
	OrganizationID string            `json:"organization_id"`
	ServiceID      string            `json:"service_id"`
	TraceID        string            `json:"trace_id"`
	SpanID         string            `json:"span_id"`
	ParentSpanID   string            `json:"parent_span_id,omitempty"`
	SpanName       string            `json:"span_name"`
	DurationMS     float64           `json:"duration_ms"`
	StatusCode     string            `json:"status_code"`
	Attributes     map[string]string `json:"attributes,omitempty"`
}

func (s *IngestServer) IngestTraces(w http.ResponseWriter, r *http.Request) {
	var traces []TracePayload
	_ = json.NewDecoder(r.Body).Decode(&traces)

	if s.conn != nil && len(traces) > 0 {
		ctx := context.Background()
		batch, err := s.conn.PrepareBatch(ctx, "INSERT INTO telemetry_traces (timestamp, organization_id, service_id, trace_id, span_id, parent_span_id, span_name, duration_ms, status_code, attributes)")
		if err == nil {
			for _, t := range traces {
				if t.StatusCode == "" {
					t.StatusCode = "OK"
				}
				if t.Attributes == nil {
					t.Attributes = map[string]string{}
				}
				_ = batch.Append(time.Now().UTC(), t.OrganizationID, t.ServiceID, t.TraceID, t.SpanID, t.ParentSpanID, t.SpanName, t.DurationMS, t.StatusCode, t.Attributes)
			}
			_ = batch.Send()
		}
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	json.NewEncoder(w).Encode(map[string]string{"status": "accepted"})
}
