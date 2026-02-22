package steam

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

// Client is a minimal Steam Web API client for CS2 match history.
type Client struct {
	apiKey     string
	httpClient *http.Client
}

// NewClient creates a Steam client authenticated with the given Steam Web API key.
func NewClient(apiKey string) *Client {
	return &Client{
		apiKey:     apiKey,
		httpClient: &http.Client{Timeout: 15 * time.Second},
	}
}

// NextShareCode returns the match sharing code that follows knownCode in the
// user's match history (oldest → newest).
//
// Returns ("", nil) when the chain is exhausted (HTTP 412 — no newer match).
// Returns an error for auth failures (HTTP 403) and other unexpected responses.
func (c *Client) NextShareCode(steamID, authCode, knownCode string) (string, error) {
	params := url.Values{
		"key":        {c.apiKey},
		"steamid":    {steamID},
		"steamidkey": {authCode},
		"knowncode":  {knownCode},
	}
	endpoint := "https://api.steampowered.com/ICSGOPlayers_730/GetNextMatchSharingCode/v1?" + params.Encode()

	resp, err := c.httpClient.Get(endpoint) //nolint:gosec
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)

	switch resp.StatusCode {
	case http.StatusOK, http.StatusAccepted: // 200 or 202
		// handled below — 202 is returned when the chain is at its tip
	case http.StatusPreconditionFailed: // 412 — chain exhausted (documented)
		return "", nil
	case http.StatusForbidden: // 403 — bad auth code
		return "", fmt.Errorf("steam: invalid auth code — generate one at Steam Settings → Account → Game Details")
	case http.StatusTooManyRequests,     // 429 — rate limited
		http.StatusServiceUnavailable: // 503 — rate limited
		return "", fmt.Errorf("steam: rate limited by Valve API (HTTP %d) — wait a minute and retry", resp.StatusCode)
	default:
		snippet := string(body)
		if len(snippet) > 200 {
			snippet = snippet[:200]
		}
		return "", fmt.Errorf("steam: HTTP %d: %s", resp.StatusCode, snippet)
	}

	var result struct {
		Result struct {
			NextCode string `json:"nextcode"`
		} `json:"result"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return "", fmt.Errorf("steam: decode response: %w", err)
	}
	// "n/a" is the API's way of saying no newer match exists (often returned
	// alongside HTTP 202 instead of the documented 412).
	if result.Result.NextCode == "" || result.Result.NextCode == "n/a" {
		return "", nil
	}
	return result.Result.NextCode, nil
}

// DemoFilename returns the filename component of a CS2 demo for the given
// share code, in the format used by Valve's replay servers.
func DemoFilename(sc ShareCode) string {
	return fmt.Sprintf("%d_%d_%d.dem.bz2", sc.MatchID, sc.ReservationID, sc.TVPort)
}
