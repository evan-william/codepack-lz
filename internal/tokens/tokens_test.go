package tokens

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestEstimatorOffline(t *testing.T) {
	c, err := NewEstimator()
	require.NoError(t, err, "heuristic estimator is local; no network involved")

	require.Equal(t, "heuristic", c.Name())
	require.True(t, c.Estimate(), "local counts are always estimates")

	n1, err := c.Count([]byte("package main\n\nfunc main() {}\n"))
	require.NoError(t, err)
	require.Greater(t, n1, 0)

	n2, err := c.Count([]byte("package main\n\nfunc main() {}\n"))
	require.NoError(t, err)
	require.Equal(t, n1, n2, "deterministic")

	zero, err := c.Count(nil)
	require.NoError(t, err)
	require.Zero(t, zero)
}

func TestAnthropicCounter(t *testing.T) {
	var gotAuth, gotVersion, gotModel string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodPost, r.Method)
		gotAuth = r.Header.Get("x-api-key")
		gotVersion = r.Header.Get("anthropic-version")
		var body struct {
			Model    string `json:"model"`
			Messages []struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			} `json:"messages"`
		}
		require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		gotModel = body.Model
		require.Equal(t, "user", body.Messages[0].Role)
		require.Equal(t, "hello", body.Messages[0].Content)
		_, _ = w.Write([]byte(`{"input_tokens":7}`))
	}))
	defer srv.Close()

	c, err := NewAnthropicCounter("test-key", "claude-test", srv.URL)
	require.NoError(t, err)
	require.False(t, c.Estimate())
	require.Equal(t, "anthropic:claude-test", c.Name())

	n, err := c.Count([]byte("hello"))
	require.NoError(t, err)
	require.Equal(t, 7, n)
	require.Equal(t, "test-key", gotAuth)
	require.Equal(t, AnthropicVersion, gotVersion)
	require.Equal(t, "claude-test", gotModel)
}
