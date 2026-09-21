package enrollment_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type liveMintResponse struct {
	AccessToken   string `json:"access_token"`
	ServicesToken string `json:"services_token"`
	ExpiresIn     int64  `json:"expires_in"`
	OperatorID    string `json:"operator_id"`
	Error         string `json:"error"`
}

func mintLive(t *testing.T, url string, payload map[string]string) liveMintResponse {
	t.Helper()
	body, err := json.Marshal(payload)
	require.NoError(t, err)

	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, url, bytes.NewReader(body))
	require.NoError(t, err)
	req.Header.Set("content-type", "application/json")

	resp, err := (&http.Client{Timeout: 15 * time.Second}).Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()

	var out liveMintResponse
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&out))
	t.Logf("mint status=%d", resp.StatusCode)
	return out
}
