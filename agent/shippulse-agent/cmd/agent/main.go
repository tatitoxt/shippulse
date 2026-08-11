package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/spf13/cobra"
)

type AgentConfig struct {
	ControlPlaneURL string `json:"control_plane_url"`
	AgentID         string `json:"agent_id"`
	AgentToken      string `json:"agent_token"`
	AgentName       string `json:"agent_name"`
	Runtime         string `json:"runtime"`
}

var (
	controlPlaneURL   string
	registrationToken string
	agentName         string
	agentToken        string
	agentID           string
)

func defaultControlPlaneURL() string {
	if value := os.Getenv("CONTROL_PLANE_URL"); value != "" {
		return value
	}
	return "http://control-plane:8080"
}

func main() {
	var rootCmd = &cobra.Command{
		Use:   "shippulse-agent",
		Short: "ShipPulse Infrastructure Ops & Deployment Agent",
	}

	var registerCmd = &cobra.Command{
		Use:   "register",
		Short: "Register agent with ShipPulse Control Plane using one-time token",
		Run:   runRegister,
	}
	registerCmd.Flags().StringVar(&controlPlaneURL, "control-plane", defaultControlPlaneURL(), "Control Plane URL")
	registerCmd.Flags().StringVar(&registrationToken, "token", "", "One-time registration token (required)")
	registerCmd.Flags().StringVar(&agentName, "name", "local-docker-agent", "Agent name")
	registerCmd.MarkFlagRequired("token")

	var runCmd = &cobra.Command{
		Use:   "run",
		Short: "Run the agent background daemon to execute deployment operations",
		Run:   runAgentDaemon,
	}
	runCmd.Flags().StringVar(&controlPlaneURL, "control-plane", defaultControlPlaneURL(), "Control Plane URL")
	runCmd.Flags().StringVar(&agentToken, "agent-token", "", "Agent auth token")
	runCmd.Flags().StringVar(&agentID, "agent-id", "", "Agent ID")

	var statusCmd = &cobra.Command{
		Use:   "status",
		Short: "Check agent status and connectivity",
		Run:   runStatus,
	}

	rootCmd.AddCommand(registerCmd, runCmd, statusCmd)

	if err := rootCmd.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}

func runRegister(cmd *cobra.Command, args []string) {
	fmt.Printf("==> Registering ShipPulse Agent '%s' with %s...\n", agentName, controlPlaneURL)

	hostname, _ := os.Hostname()
	payload := map[string]interface{}{
		"registration_token": registrationToken,
		"name":               agentName,
		"hostname":           hostname,
		"runtime_type":       "docker",
		"version":            "1.0.0",
		"capabilities":       []string{"docker:deploy", "docker:rollback", "health:check"},
	}

	data, _ := json.Marshal(payload)
	resp, err := http.Post(controlPlaneURL+"/api/v1/agents/register", "application/json", bytes.NewBuffer(data))
	if err != nil {
		log.Fatalf("Registration failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		log.Fatalf("Registration rejected (HTTP %d): %s", resp.StatusCode, string(body))
	}

	var result struct {
		AgentID    string `json:"agent_id"`
		AgentToken string `json:"agent_token"`
		Status     string `json:"status"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&result)

	// Save configuration locally
	cfg := AgentConfig{
		ControlPlaneURL: controlPlaneURL,
		AgentID:         result.AgentID,
		AgentToken:      result.AgentToken,
		AgentName:       agentName,
		Runtime:         "docker",
	}

	cfgData, _ := json.MarshalIndent(cfg, "", "  ")
	_ = os.WriteFile("agent-config.json", cfgData, 0600)

	fmt.Printf("==> Agent successfully registered!\nAgent ID: %s\nConfiguration saved to agent-config.json\n", result.AgentID)
}

func runAgentDaemon(cmd *cobra.Command, args []string) {
	// Try loading from local config file if flags empty
	if agentToken == "" || agentID == "" {
		cfgData, err := os.ReadFile("agent-config.json")
		if err == nil {
			var cfg AgentConfig
			if err := json.Unmarshal(cfgData, &cfg); err == nil {
				if controlPlaneURL == defaultControlPlaneURL() && cfg.ControlPlaneURL != "" {
					controlPlaneURL = cfg.ControlPlaneURL
				}
				agentToken = cfg.AgentToken
				agentID = cfg.AgentID
			}
		}
	}

	fmt.Println("==> Starting ShipPulse Agent Daemon...")
	fmt.Printf("Control Plane: %s | Agent ID: %s\n", controlPlaneURL, agentID)

	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	client := &http.Client{Timeout: 5 * time.Second}

	for range ticker.C {
		// Send Heartbeat
		payload := map[string]interface{}{
			"agent_id": agentID,
			"status":   "ONLINE",
		}
		data, _ := json.Marshal(payload)

		req, err := http.NewRequest("POST", controlPlaneURL+"/api/v1/agents/heartbeat", bytes.NewBuffer(data))
		if err == nil {
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("X-API-Key", agentToken)
			resp, err := client.Do(req)
			if err == nil {
				resp.Body.Close()
				log.Println("Agent heartbeat acknowledged.")
			} else {
				log.Printf("Heartbeat error: %v", err)
			}
		}
	}
}

func runStatus(cmd *cobra.Command, args []string) {
	cfgData, err := os.ReadFile("agent-config.json")
	if err != nil {
		fmt.Println("Agent is not registered. Run 'shippulse-agent register' first.")
		return
	}

	var cfg AgentConfig
	_ = json.Unmarshal(cfgData, &cfg)

	fmt.Printf("ShipPulse Agent Status:\n")
	fmt.Printf("Name: %s\nID: %s\nControl Plane: %s\nStatus: Configured\n", cfg.AgentName, cfg.AgentID, cfg.ControlPlaneURL)
}
