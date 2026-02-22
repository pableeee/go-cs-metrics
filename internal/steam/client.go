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

	body, _ := io.ReadAll(resp.Body)

	switch resp.StatusCode {
	case http.StatusOK, http.StatusAccepted: // 200 or 202
		// handled below — 202 is returned when the chain is at its tip
	case http.StatusPreconditionFailed: // 412 — chain exhausted (documented)
		return "", nil
	case http.StatusForbidden: // 403 — bad auth code
		return "", fmt.Errorf("steam: invalid auth code — generate one at Steam Settings → Account → Game Details")
	case http.StatusServiceUnavailable: // 503 — rate limited
		return "", fmt.Errorf("steam: rate limited by Valve API, wait a moment and retry")
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

// ReplayURLPattern returns the URL template being probed for a share code
// (with server number N=1 as a representative sample). Useful for manual debugging.
func ReplayURLPattern(sc ShareCode) string {
	return fmt.Sprintf("http://replay1.valve.net/730/%d_%d_%d.dem.bz2",
		sc.MatchID, sc.ReservationID, sc.TVPort)
}

// ResolveReplayURL probes Valve's replay server fleet to find the download URL
// for the given share code. Demos are hosted at:
//
//	http://replay{N}.valve.net/730/{matchID}_{reservationID}_{tvPort}.dem.bz2
//
// The server number N is not publicly derivable without Game Coordinator access,
// so we probe servers 1–150 concurrently. HEAD requests are avoided because some
// Valve servers silently drop them; instead we use GET with Range: bytes=0-0
// which downloads nothing but reliably exercises the request path.
// Returns an error if no server has the file (demo may have expired).
func ResolveReplayURL(sc ShareCode) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	found := make(chan string, 1)
	var once sync.Once
	var wg sync.WaitGroup

	probeClient := &http.Client{Timeout: 8 * time.Second}

	for n := 1; n <= 150; n++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()

			u := fmt.Sprintf("http://replay%d.valve.net/730/%d_%d_%d.dem.bz2",
				n, sc.MatchID, sc.ReservationID, sc.TVPort)

			req, err := http.NewRequestWithContext(ctx, "GET", u, nil)
			if err != nil {
				return
			}
			// Request only the first byte so we don't download the demo here.
			req.Header.Set("Range", "bytes=0-0")

			resp, err := probeClient.Do(req)
			if err != nil {
				return
			}
			io.Copy(io.Discard, resp.Body) //nolint:errcheck
			resp.Body.Close()

			// 200 OK or 206 Partial Content both mean the file exists.
			if resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusPartialContent {
				once.Do(func() {
					select {
					case found <- u:
					default:
					}
					cancel()
				})
			}
		}(n)
	}

	go func() {
		wg.Wait()
		close(found)
	}()

	u, ok := <-found
	if !ok {
		sample := ReplayURLPattern(sc)
		return "", fmt.Errorf("demo not found on any Valve replay server (servers 1–150)\n"+
			"  Verify the URL format manually: curl -I %q\n"+
			"  If curl returns 404 on all servers, the demo may have expired (kept ~30 days)",
			sample)
	}
	return u, nil
}
