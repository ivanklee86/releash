//go:build integration

// Package integration holds helpers for the integration tests, which run
// against the local Unleash stack from docker-compose.yaml. Start the stack
// with `task services` and run the tests with `task go:test-integration`.
//
// Every file is behind the integration build tag, so plain `go test ./...`
// neither compiles nor runs any of it.
package integration

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"
)

// Environment names of the local stack. Each has its own client token.
const (
	Development = "development"
	Production  = "production"
)

// Environments lists every environment of the local stack.
var Environments = []string{Development, Production}

// Defaults are the fixed dev-only values from docker-compose.yaml and
// .env.example. They are not secrets and only work on the local stack.
// DefaultURL is the API URL, with /api, as UNLEASH_URL is everywhere.
const (
	DefaultURL              = "http://localhost:4242/api"
	DefaultAdminToken       = "*:*.releash-dev-admin-not-a-secret"
	DefaultDevelopmentToken = "default:development.releash-dev-not-a-secret"
	DefaultProductionToken  = "default:production.releash-dev-not-a-secret"
)

// ScenarioFlagPrefix starts the name of every flag that a scenario creates
// through the Admin API. Such a flag must be archived in t.Cleanup. The smoke
// test ignores flags with this prefix, so scenarios running in parallel with it
// do not break it.
const ScenarioFlagPrefix = "scenario-"

// URL returns the Unleash API URL, including /api as in spec 0001, from
// UNLEASH_URL, or DefaultURL when the variable is unset or empty. A trailing
// slash is removed, so Client API paths can be appended to it, e.g.
// URL()+"/client/features".
func URL() string {
	return strings.TrimSuffix(envOr("UNLEASH_URL", DefaultURL), "/")
}

// AdminToken returns the Admin API token from UNLEASH_ADMIN_TOKEN, or
// DefaultAdminToken when the variable is unset or empty.
func AdminToken() string {
	return envOr("UNLEASH_ADMIN_TOKEN", DefaultAdminToken)
}

// ClientToken returns the client (backend) token for env, which must be
// Development or Production. It reads UNLEASH_DEVELOPMENT_TOKEN or
// UNLEASH_PRODUCTION_TOKEN and falls back to the compose default when the
// variable is unset or empty. It fails the test for any other env.
func ClientToken(t testing.TB, env string) string {
	t.Helper()
	switch env {
	case Development:
		return envOr("UNLEASH_DEVELOPMENT_TOKEN", DefaultDevelopmentToken)
	case Production:
		return envOr("UNLEASH_PRODUCTION_TOKEN", DefaultProductionToken)
	default:
		t.Fatalf("no client token for environment %q; want one of %v", env, Environments)
		return ""
	}
}

// RequireUnleash fails the test unless Unleash answers its health check at
// the server root (URL without /api) within a few seconds. Call it first in
// every test that talks to the stack, so a stopped stack gives one clear
// message instead of a timeout.
func RequireUnleash(t testing.TB) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()

	if err := checkHealth(ctx, serverRoot(URL())); err != nil {
		t.Fatalf("Unleash is not reachable at %s: %v\nStart the local stack with `task services`.", URL(), err)
	}
}

// serverRoot returns apiURL without its trailing /api. Unleash serves /health
// at the server root, and /api/health needs a token.
func serverRoot(apiURL string) string {
	return strings.TrimSuffix(strings.TrimSuffix(apiURL, "/"), "/api")
}

func checkHealth(ctx context.Context, baseURL string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/health", nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("GET /health returned %s", resp.Status)
	}
	return nil
}

func envOr(name, fallback string) string {
	if v := os.Getenv(name); v != "" {
		return v
	}
	return fallback
}
