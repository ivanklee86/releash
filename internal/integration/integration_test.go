//go:build integration

package integration_test

import (
	"encoding/json"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ivanklee86/releash/internal/integration"
)

// clientFeatures is the part of a Client API response the smoke test reads.
type clientFeatures struct {
	Features []struct {
		Name    string `json:"name"`
		Project string `json:"project"`
		Enabled bool   `json:"enabled"`
	} `json:"features"`
}

// TestClientTokensSeeTheFixtureFlags checks that the stack is seeded as
// unleash/fixtures.json says: each client token sees every fixture flag, and
// each flag is switched on or off as the fixture says for that token's
// environment. The release scenarios in M3 and M4 rely on this.
func TestClientTokensSeeTheFixtureFlags(t *testing.T) {
	integration.RequireUnleash(t)
	fixtures := integration.LoadFixtures(t)
	require.NotEmpty(t, fixtures.Flags, "unleash/fixtures.json has no flags")

	for _, env := range integration.Environments {
		t.Run(env, func(t *testing.T) {
			got := fetchClientFeatures(t, integration.ClientToken(t, env))

			enabled := make(map[string]bool, len(got.Features))
			for _, f := range got.Features {
				assert.Equal(t, fixtures.Project, f.Project, "project of flag %q", f.Name)
				enabled[f.Name] = f.Enabled
			}
			for _, flag := range fixtures.Flags {
				on, ok := enabled[flag.Name]
				if !assert.True(t, ok, "flag %q is not visible to the %s token", flag.Name, env) {
					continue
				}
				assert.Equal(t, flag.EnabledIn(env), on, "flag %q enabled in %s", flag.Name, env)
			}
			assert.Len(t, got.Features, len(fixtures.Flags), "the %s token sees flags that are not fixtures", env)
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

func isTimeout(err error) bool {
	var te interface{ Timeout() bool }
	return errors.As(err, &te) && te.Timeout()
}

func fetchClientFeatures(t *testing.T, token string) clientFeatures {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, integration.URL()+"/api/client/features", nil)
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
