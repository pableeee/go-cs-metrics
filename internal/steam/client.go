package steam

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sync"
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

	switch resp.StatusCode {
	case http.StatusOK:
		// handled below
	case http.StatusPreconditionFailed: // 412 — chain exhausted
		return "", nil
	case http.StatusForbidden: // 403 — bad auth code
		return "", fmt.Errorf("steam: invalid auth code — generate one at Steam Settings → Account → Game Details")
	case http.StatusServiceUnavailable: // 503 — rate limited
		return "", fmt.Errorf("steam: rate limited by Valve API, wait a moment and retry")
	default:
		body, _ := io.ReadAll(resp.Body)
		snippet := string(body)
		if len(snippet) > 200 {
			snippet = snippet[:200]
		}
		return "", fmt.Errorf("steam: HTTP %d: %s", resp.StatusCode, snippet)
	}

	body, _ := io.ReadAll(resp.Body)
	var result struct {
		Result struct {
			NextCode string `json:"nextcode"`
		} `json:"result"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return "", fmt.Errorf("steam: decode response: %w", err)
	}
	return result.Result.NextCode, nil
}

// ResolveReplayURL probes Valve's replay server fleet to find the download URL
// for the given share code. Demos are hosted at:
//
//	http://replay{N}.valve.net/730/{matchID}_{reservationID}_{tvPort}.dem.bz2
//
// The server number N is not publicly derivable without Game Coordinator access,
// so we probe servers 1–120 concurrently and return the first responding URL.
// Returns an error if no server has the file (demo may have expired).
func ResolveReplayURL(sc ShareCode) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	found := make(chan string, 1)
	var once sync.Once
	var wg sync.WaitGroup

	probeClient := &http.Client{Timeout: 6 * time.Second}

	for n := 1; n <= 120; n++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()

			u := fmt.Sprintf("http://replay%d.valve.net/730/%d_%d_%d.dem.bz2",
				n, sc.MatchID, sc.ReservationID, sc.TVPort)

			req, err := http.NewRequestWithContext(ctx, "HEAD", u, nil)
			if err != nil {
				return
			}

			resp, err := probeClient.Do(req)
			if err != nil {
				return
			}
			resp.Body.Close()

			if resp.StatusCode == http.StatusOK {
				once.Do(func() {
					select {
					case found <- u:
					default:
					}
					cancel() // cancel all remaining probes
				})
			}
		}(n)
	}

	// Close the found channel once all goroutines have exited so the receive
	// below unblocks even when no server responded.
	go func() {
		wg.Wait()
		close(found)
	}()

	u, ok := <-found
	if !ok {
		return "", fmt.Errorf("demo not found on any Valve replay server — it may have expired (demos are kept for ~30 days)")
	}
	return u, nil
}
