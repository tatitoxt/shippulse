// Command shippulse runs every ShipPulse process from a single binary:
//
//	shippulse migrate      apply database migrations
//	shippulse seed         load the synthetic demo catalog
//	shippulse api          serve the HTTP API
//	shippulse worker       relay the outbox and execute deployment/rollback jobs
//	shippulse simruntime   serve the simulated deployment target
//	shippulse demo         drive the scripted demo against a running API
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/tatitoxt/shippulse/internal/api"
	"github.com/tatitoxt/shippulse/internal/auth"
	"github.com/tatitoxt/shippulse/internal/bus"
	"github.com/tatitoxt/shippulse/internal/config"
	"github.com/tatitoxt/shippulse/internal/demo"
	"github.com/tatitoxt/shippulse/internal/migrate"
	"github.com/tatitoxt/shippulse/internal/outbox"
	"github.com/tatitoxt/shippulse/internal/provider"
	"github.com/tatitoxt/shippulse/internal/seed"
	"github.com/tatitoxt/shippulse/internal/simruntime"
	"github.com/tatitoxt/shippulse/internal/store"
	"github.com/tatitoxt/shippulse/internal/worker"
)

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: shippulse <migrate|seed|api|worker|simruntime|demo>")
		os.Exit(2)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	var err error
	switch cmd := os.Args[1]; cmd {
	case "migrate":
		err = runMigrate(ctx, log)
	case "seed":
		err = runSeed(ctx, log)
	case "api":
		err = runAPI(ctx, log)
	case "worker":
		err = runWorker(ctx, log)
	case "simruntime":
		err = serve(ctx, log, config.String("SIMRUNTIME_ADDR", ":8090"), simruntime.Handler(simruntime.New()))
	case "probe":
		// Container health check: exits non-zero unless the URL answers 2xx.
		err = probe(os.Args[2:])
	case "demo":
		err = demo.Run(ctx, os.Stdout, demo.Config{
			APIURL:        config.String("SHIPPULSE_API_URL", "http://localhost:8080"),
			Email:         config.String("DEMO_EMAIL", "operator@acme.example"),
			Password:      os.Getenv("DEMO_PASSWORD"),
			WebhookSecret: os.Getenv("GITHUB_WEBHOOK_SECRET"),
			Timeout:       config.Duration("DEMO_TIMEOUT", 90*time.Second),
		})
	default:
		err = fmt.Errorf("unknown command %q", cmd)
	}
	if err != nil {
		log.Error("exit", "cmd", os.Args[1], "err", err)
		os.Exit(1)
	}
}

func connect(ctx context.Context) (*store.Store, error) {
	pool, err := store.Connect(ctx, config.DatabaseURL())
	if err != nil {
		return nil, err
	}
	return store.New(pool), nil
}

func runMigrate(ctx context.Context, log *slog.Logger) error {
	s, err := connect(ctx)
	if err != nil {
		return err
	}
	defer s.Pool.Close()
	v, err := migrate.Up(ctx, s.Pool)
	if err == nil {
		log.Info("migrations applied", "version", v)
	}
	return err
}

func runSeed(ctx context.Context, log *slog.Logger) error {
	s, err := connect(ctx)
	if err != nil {
		return err
	}
	defer s.Pool.Close()
	if err := seed.Run(ctx, s.Pool, os.Getenv("DEMO_PASSWORD")); err != nil {
		return err
	}
	log.Info("demo data seeded", "tenants", len(seed.Tenants))
	return nil
}

func runAPI(ctx context.Context, log *slog.Logger) error {
	issuer, err := auth.NewIssuer(os.Getenv("JWT_SECRET"), config.Duration("JWT_TTL", 8*time.Hour))
	if err != nil {
		return err
	}
	s, err := connect(ctx)
	if err != nil {
		return err
	}
	defer s.Pool.Close()
	srv := &api.Server{
		Store: s, Auth: issuer, Log: log,
		Runtime:       provider.NewHTTP(config.RuntimeURL()),
		WebhookSecret: os.Getenv("GITHUB_WEBHOOK_SECRET"),
	}
	return serve(ctx, log, config.String("API_ADDR", ":8080"), srv.Router())
}

func runWorker(ctx context.Context, log *slog.Logger) error {
	s, err := connect(ctx)
	if err != nil {
		return err
	}
	defer s.Pool.Close()
	b, err := bus.Connect(ctx, config.NATSURL())
	if err != nil {
		return err
	}
	defer b.Close()

	host, _ := os.Hostname()
	cfg := worker.DefaultConfig(config.String("WORKER_ID", host))
	cfg.Probe.Attempts = config.Int("HEALTH_PROBE_ATTEMPTS", cfg.Probe.Attempts)
	cfg.Probe.Timeout = config.Duration("HEALTH_PROBE_TIMEOUT", cfg.Probe.Timeout)
	cfg.Probe.Interval = config.Duration("HEALTH_PROBE_INTERVAL", cfg.Probe.Interval)
	exec := &worker.Executor{Store: s, Provider: provider.NewHTTP(config.RuntimeURL()), Cfg: cfg, Log: log}

	relay := &outbox.Relay{Store: s, Publish: b.Publish, Interval: 250 * time.Millisecond, Batch: 50,
		Base: 500 * time.Millisecond, Max: 30 * time.Second, Log: log}
	go relay.Run(ctx)

	cc, err := b.Consume(ctx, log, exec.Handle)
	if err != nil {
		return err
	}
	defer cc.Stop()
	log.Info("worker started", "worker_id", cfg.WorkerID)
	<-ctx.Done()
	return nil
}

func serve(ctx context.Context, log *slog.Logger, addr string, h http.Handler) error {
	srv := &http.Server{Addr: addr, Handler: h, ReadHeaderTimeout: 5 * time.Second}
	errc := make(chan error, 1)
	go func() { errc <- srv.ListenAndServe() }()
	log.Info("listening", "addr", addr)
	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
		shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutdown); err != nil && !errors.Is(err, http.ErrServerClosed) {
			return err
		}
		return nil
	}
}

func probe(args []string) error {
	if len(args) != 1 {
		return errors.New("usage: shippulse probe <url>")
	}
	c := &http.Client{Timeout: 2 * time.Second}
	resp, err := c.Get(args[0])
	if err != nil {
		return err
	}
	resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("probe %s: status %d", args[0], resp.StatusCode)
	}
	return nil
}
