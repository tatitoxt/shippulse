package provider

import (
	"context"
	"errors"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/tatitoxt/shippulse/internal/domain"
	"github.com/tatitoxt/shippulse/internal/simruntime"
)

var fast = ProbeConfig{Attempts: 3, Timeout: 30 * time.Millisecond, Interval: time.Millisecond}

func providers(t *testing.T) map[string]Provider {
	srv := httptest.NewServer(simruntime.Handler(simruntime.New()))
	t.Cleanup(srv.Close)
	return map[string]Provider{"memory": NewMemory(), "http": NewHTTP(srv.URL)}
}

func TestProbeProfiles(t *testing.T) {
	for name, p := range providers(t) {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			tg := Target{Service: "checkout", Environment: "staging"}
			cases := []struct {
				version string
				profile domain.FailureProfile
				code    string
				tries   int
			}{
				{"v1", domain.ProfileHealthy, "", 1},
				{"v2", domain.ProfileUnhealthy, domain.CodeHealthCheckFailed, 3},
				{"v3", domain.ProfileTimeout, domain.CodeHealthCheckTimeout, 3},
			}
			for _, c := range cases {
				if err := p.Apply(ctx, tg, c.version, c.profile); err != nil {
					t.Fatalf("apply %s: %v", c.version, err)
				}
				tries, code, _ := ProbeUntilHealthy(ctx, p, tg, fast)
				if code != c.code || tries != c.tries {
					t.Errorf("%s: got code=%q tries=%d, want %q/%d", c.profile, code, tries, c.code, c.tries)
				}
			}
			if err := p.Apply(ctx, tg, "v4", domain.ProfileProviderUnavailable); !errors.Is(err, ErrUnavailable) {
				t.Fatalf("want ErrUnavailable, got %v", err)
			}
			st, err := p.Status(ctx, tg)
			if err != nil || st.Version != "v3" {
				t.Fatalf("status after unavailable apply: %+v %v", st, err)
			}
		})
	}
}

func TestHTTPProviderUnreachable(t *testing.T) {
	p := NewHTTP("http://127.0.0.1:1")
	if err := p.Apply(context.Background(), Target{"a", "b"}, "v1", domain.ProfileHealthy); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("want ErrUnavailable, got %v", err)
	}
}
