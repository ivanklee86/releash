//go:build integration

package integration_test

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ivanklee86/releash/internal/integration"
)

// clientFeatures is the part of a Client API response the smoke test reads.
type clientFeatures struct {
	Features []struct {
		Name       string                 `json:"name"`
		Project    string                 `json:"project"`
		Enabled    bool                   `json:"enabled"`
		Strategies []integration.Strategy `json:"strategies"`
		Variants   []integration.Variant  `json:"variants"`
	} `json:"features"`
}

// TestClientTokensSeeTheFixtureFlags checks that the stack is seeded as
// unleash/fixtures.json says: each client token sees every fixture flag and no
// other flag apart from scenario flags (integration.ScenarioFlagPrefix), each
// flag is switched on or off as the fixture says for that token's environment,
// its strategies have the fixture's names, parameters, constraints and
// variants, and its feature variants are the fixture's, payloads and weights
// included. The release scenarios rely on this.
func TestClientTokensSeeTheFixtureFlags(t *testing.T) {
	integration.RequireUnleash(t)
	fixtures := integration.LoadFixtures(t)
	require.NotEmpty(t, fixtures.Flags, "unleash/fixtures.json has no flags")

	for _, env := range integration.Environments {
		t.Run(env, func(t *testing.T) {
			got := fetchClientFeatures(t, integration.ClientToken(t, env))

			byName := make(map[string]int, len(got.Features))
			for i, f := range got.Features {
				if strings.HasPrefix(f.Name, integration.ScenarioFlagPrefix) {
					continue
				}
				assert.Equal(t, fixtures.Project, f.Project, "project of flag %q", f.Name)
				byName[f.Name] = i
			}
			for _, flag := range fixtures.Flags {
				i, ok := byName[flag.Name]
				if !assert.True(t, ok, "flag %q is not visible to the %s token", flag.Name, env) {
					continue
				}
				f := got.Features[i]
				assert.Equal(t, flag.EnabledIn(env), f.Enabled, "flag %q enabled in %s", flag.Name, env)
				assert.ElementsMatch(t, normalize(flag.Strategies), normalize(f.Strategies), "strategies of flag %q in %s", flag.Name, env)
				assert.ElementsMatch(t, normalizeVariants(flag.Variants), normalizeVariants(f.Variants), "variants of flag %q in %s", flag.Name, env)
			}
			assert.Len(t, byName, len(fixtures.Flags), "the %s token sees flags that are neither fixtures nor scenario flags", env)
		})
	}
}

// TestUnresponsiveServerNeverAnswers checks the fake server that the
// unresponsive-Unleash scenarios use: a request to it hangs until the
// client's timeout.
func TestUnresponsiveServerNeverAnswers(t *testing.T) {
	url := integration.UnresponsiveServer(t)
	client := &http.Client{Timeout: 200 * time.Millisecond}

	start := time.Now()
	resp, err := client.Get(url + "/api/client/features")
	if resp != nil {
		_ = resp.Body.Close()
	}

	require.Error(t, err)
	assert.True(t, isTimeout(err), "want a timeout, got %v", err)
	assert.GreaterOrEqual(t, time.Since(start), 200*time.Millisecond)
}

// normalize drops the empty lists and maps that the Client API sends and
// fixtures.json leaves out, so the two compare equal when they mean the same.
func normalize(strategies []integration.Strategy) []integration.Strategy {
	out := make([]integration.Strategy, 0, len(strategies))
	for _, s := range strategies {
		if len(s.Parameters) == 0 {
			s.Parameters = nil
		}
		constraints := make([]integration.Constraint, 0, len(s.Constraints))
		for _, c := range s.Constraints {
			if len(c.Values) == 0 {
				c.Values = nil
			}
			constraints = append(constraints, c)
		}
		s.Constraints = nil
		if len(constraints) > 0 {
			s.Constraints = constraints
		}
		s.Variants = normalizeVariants(s.Variants)
		out = append(out, s)
	}
	return out
}

// normalizeVariants fills in the stickiness Unleash stores for a variant
// without one and drops an empty list, so fixture and Client API variants
// compare equal when they mean the same.
func normalizeVariants(variants []integration.Variant) []integration.Variant {
	if len(variants) == 0 {
		return nil
	}
	out := make([]integration.Variant, 0, len(variants))
	for _, v := range variants {
		if v.Stickiness == "" {
			v.Stickiness = "default"
		}
		out = append(out, v)
	}
	return out
}

func isTimeout(err error) bool {
	var te interface{ Timeout() bool }
	return errors.As(err, &te) && te.Timeout()
}

func fetchClientFeatures(t *testing.T, token string) clientFeatures {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, integration.URL()+"/client/features", nil)
	require.NoError(t, err)
	req.Header.Set("Authorization", token)

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	require.Equal(t, http.StatusOK, resp.StatusCode, "GET /api/client/features")

	var got clientFeatures
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&got))
	return got
}
