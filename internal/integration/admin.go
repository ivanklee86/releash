//go:build integration

package integration

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"
)

// CreateFlag creates a flag in the fixtures' project through the Admin API
// and returns its name. The flag has the given strategies in every
// environment and is switched on in each. Its name is ScenarioFlagPrefix,
// then a slug of the test name and a random suffix, so parallel and repeated
// runs never collide. CreateFlag waits until both client tokens see the flag
// with all its strategies, and archives the flag when the test ends. It
// fails the test on any Admin API error.
func CreateFlag(t testing.TB, strategies ...Strategy) string {
	t.Helper()
	name := scenarioFlagName(t)
	base := "/api/admin/projects/default/features/" + url.PathEscape(name)

	adminCall(t, http.MethodPost, "/api/admin/projects/default/features", map[string]string{"name": name, "type": "release"})
	t.Cleanup(func() {
		// Archive, the Admin API's DELETE, even when the test failed.
		if err := adminRequest(http.MethodDelete, base, nil); err != nil {
			t.Errorf("archiving scenario flag %s: %v", name, err)
		}
	})
	for _, env := range Environments {
		for _, s := range strategies {
			adminCall(t, http.MethodPost, base+"/environments/"+env+"/strategies", s)
		}
		adminCall(t, http.MethodPost, base+"/environments/"+env+"/on", nil)
	}
	for _, env := range Environments {
		waitForClientFlag(t, ClientToken(t, env), name, len(strategies))
	}
	return name
}

// RolloutWith returns a 100% flexibleRollout strategy whose constraints must
// all match, the shape every fixture flag uses.
func RolloutWith(constraints ...Constraint) Strategy {
	return Strategy{
		Name:        "flexibleRollout",
		Parameters:  map[string]string{"rollout": "100", "stickiness": "default", "groupId": "scenario"},
		Constraints: constraints,
	}
}

var notSlug = regexp.MustCompile(`[^a-z0-9]+`)

func scenarioFlagName(t testing.TB) string {
	t.Helper()
	slug := strings.Trim(notSlug.ReplaceAllString(strings.ToLower(t.Name()), "-"), "-")
	if len(slug) > 60 {
		slug = slug[:60]
	}
	suffix := make([]byte, 4)
	if _, err := rand.Read(suffix); err != nil {
		t.Fatalf("naming scenario flag: %v", err)
	}
	return ScenarioFlagPrefix + slug + "-" + hex.EncodeToString(suffix)
}

// waitForClientFlag polls the Client API with token until it lists name with
// at least n strategies. Unleash serves client features from a cache that is
// refreshed shortly after a change.
func waitForClientFlag(t testing.TB, token, name string, n int) {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for {
		ok, err := clientSeesFlag(token, name, n)
		if err == nil && ok {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("scenario flag %s did not appear in the Client API within 15s (last error: %v)", name, err)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func clientSeesFlag(token, name string, n int) (bool, error) {
	req, err := http.NewRequest(http.MethodGet, URL()+"/client/features", nil)
	if err != nil {
		return false, err
	}
	req.Header.Set("Authorization", token)
	resp, err := (&http.Client{Timeout: 5 * time.Second}).Do(req)
	if err != nil {
		return false, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return false, fmt.Errorf("GET /api/client/features returned %s", resp.Status)
	}
	var got struct {
		Features []struct {
			Name       string            `json:"name"`
			Enabled    bool              `json:"enabled"`
			Strategies []json.RawMessage `json:"strategies"`
		} `json:"features"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		return false, err
	}
	for _, f := range got.Features {
		if f.Name == name {
			return f.Enabled && len(f.Strategies) >= n, nil
		}
	}
	return false, nil
}

// adminCall sends body as JSON to the Admin API at path, relative to the
// server root, and fails the test unless the answer is 2xx.
func adminCall(t testing.TB, method, path string, body any) {
	t.Helper()
	if err := adminRequest(method, path, body); err != nil {
		t.Fatal(err)
	}
}

func adminRequest(method, path string, body any) error {
	var r io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return err
		}
		r = bytes.NewReader(data)
	}
	req, err := http.NewRequest(method, serverRoot(URL())+path, r)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", AdminToken())
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := (&http.Client{Timeout: 10 * time.Second}).Do(req)
	if err != nil {
		return fmt.Errorf("%s %s: %w", method, path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return fmt.Errorf("%s %s: %s: %s", method, path, resp.Status, msg)
	}
	return nil
}
