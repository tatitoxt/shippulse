package simruntime

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/tatitoxt/shippulse/internal/domain"
)

// Handler exposes the runtime over HTTP so the worker talks to it like a real
// provider API:
//
//	PUT  /v1/runtimes/{tenant}/{service}/{env}         {"version": "...", "profile": "..."}
//	GET  /v1/runtimes/{tenant}/{service}/{env}
//	GET  /v1/runtimes/{tenant}/{service}/{env}/health
func Handler(rt *Runtime) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
	mux.HandleFunc("PUT /v1/runtimes/{tenant}/{service}/{env}", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Version string `json:"version"`
			Profile string `json:"profile"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&body); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid json"})
			return
		}
		profile, err := domain.ParseFailureProfile(body.Profile)
		if err != nil || domain.ValidateVersion(body.Version) != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid version or profile"})
			return
		}
		changed, err := rt.Apply(r.PathValue("tenant"), r.PathValue("service"), r.PathValue("env"), body.Version, profile)
		if errors.Is(err, ErrUnavailable) {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": err.Error()})
			return
		}
		st, _ := rt.Status(r.PathValue("tenant"), r.PathValue("service"), r.PathValue("env"))
		writeJSON(w, http.StatusOK, map[string]any{"changed": changed, "status": st})
	})
	mux.HandleFunc("GET /v1/runtimes/{tenant}/{service}/{env}", func(w http.ResponseWriter, r *http.Request) {
		st, err := rt.Status(r.PathValue("tenant"), r.PathValue("service"), r.PathValue("env"))
		if err != nil {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, st)
	})
	mux.HandleFunc("GET /v1/runtimes/{tenant}/{service}/{env}/health", func(w http.ResponseWriter, r *http.Request) {
		err := rt.Health(r.Context(), r.PathValue("tenant"), r.PathValue("service"), r.PathValue("env"))
		switch {
		case err == nil:
			writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
		case errors.Is(err, ErrNoRuntime):
			writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		default:
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "unhealthy", "error": err.Error()})
		}
	})
	return mux
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
