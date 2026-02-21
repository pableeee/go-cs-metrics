// Package faceit provides a minimal client for the FACEIT Data API v4.
package faceit

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// baseURL is the root endpoint for the FACEIT Data API v4.
const baseURL = "https://open.faceit.com/data/v4"

// Client is a minimal FACEIT Data API v4 client.
type Client struct {
	apiKey string
	http   *http.Client
}

// NewClient returns a FACEIT API client authenticated with the given API key.
func NewClient(apiKey string) *Client {
	return &Client{
		apiKey: apiKey,
		http:   &http.Client{Timeout: 30 * time.Second},
	}
}

// Player holds the fields we need from the /players endpoint.
type Player struct {
	PlayerID string `json:"player_id"`
	Nickname string `json:"nickname"`
	Games    struct {
		CS2 struct {
			SkillLevel int    `json:"skill_level"`
			FaceitELO  int    `json:"faceit_elo"`
			Region     string `json:"region"`
		} `json:"cs2"`
	} `json:"games"`
}

// MatchHistoryItem is one entry from /players/{id}/history.
type MatchHistoryItem struct {
	MatchID    string `json:"match_id"`
	Status     string `json:"status"`
	StartedAt  int64  `json:"started_at"`
	FinishedAt int64  `json:"finished_at"`
}

// MatchDetail holds the fields we need from /matches/{id}.
type MatchDetail struct {
	MatchID    string   `json:"match_id"`
	SkillLevel int      `json:"skill_level"`
	DemoURLs   []string `json:"demo_url"`
	StartedAt  int64    `json:"started_at"`
	Voting     struct {
		Map struct {
			Pick []string `json:"pick"`
		} `json:"map"`
	} `json:"voting"`
}

// MapName returns the picked map name, or empty string if unavailable.
func (m *MatchDetail) MapName() string {
	if len(m.Voting.Map.Pick) > 0 {
		return m.Voting.Map.Pick[0]
	}
	return ""
}

// get performs an authenticated GET request against the FACEIT API and
// JSON-decodes the response body into out.
func (c *Client) get(path string, out interface{}) error {
	req, err := http.NewRequest("GET", baseURL+path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("GET %s: %w", path, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("GET %s: HTTP %d", path, resp.StatusCode)
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

// GetPlayerByNickname looks up a player by their FACEIT nickname.
func (c *Client) GetPlayerByNickname(nickname string) (*Player, error) {
	var p Player
	if err := c.get("/players?nickname="+nickname, &p); err != nil {
		return nil, err
	}
	return &p, nil
}

// GetPlayerBySteamID looks up a player by their Steam ID64.
func (c *Client) GetPlayerBySteamID(steamID string) (*Player, error) {
	var p Player
	if err := c.get("/players?game=cs2&game_player_id="+steamID, &p); err != nil {
		return nil, err
	}
	return &p, nil
}

// GetMatchHistory returns up to limit recent finished matches for a player.
func (c *Client) GetMatchHistory(playerID string, limit int) ([]MatchHistoryItem, error) {
	var resp struct {
		Items []MatchHistoryItem `json:"items"`
	}
	path := fmt.Sprintf("/players/%s/history?game=cs2&offset=0&limit=%d", playerID, limit)
	if err := c.get(path, &resp); err != nil {
		return nil, err
	}
	return resp.Items, nil
}

// GetMatch returns details for a single match, including demo URLs and map.
func (c *Client) GetMatch(matchID string) (*MatchDetail, error) {
	var m MatchDetail
	if err := c.get("/matches/"+matchID, &m); err != nil {
		return nil, err
	}
	return &m, nil
}
