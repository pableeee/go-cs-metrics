# Demo Download Automation

This document explains why automated demo downloads are currently non-functional,
what authentication barriers block them, and the concrete steps needed to re-enable
each path once those barriers are resolved.

The code that implements each download path is preserved in `cmd/fetch.go` (FACEIT)
and `cmd/fetchmm.go` (Valve MM/Premier). Neither command is registered in the CLI.
Once the auth issues described below are resolved, the commands can be re-enabled by
adding `rootCmd.AddCommand(fetchCmd)` and `rootCmd.AddCommand(fetchMMCmd)` in
`cmd/root.go`.

---

## Path 1 — FACEIT Demos

### Current status: non-functional (two independent blockers)

**Blocker 1: CDN decommissioned**

Demo files were hosted at `demos-us-east.backblaze.faceit-cdn.net`. This domain no
longer resolves — FACEIT decommissioned the Backblaze-backed CDN at some point in 2024
or 2025. DNS queries return no records (effectively dead).

**Blocker 2: Downloads API key scope**

The FACEIT Downloads v2 endpoint
(`open.faceit.com/download/v2/demos/download`) returns:

```json
{"code":"err_f0","message":"no valid scope provided"}
```

This means the API key stored in `~/.csmetrics/faceit_api_key` (or `FACEIT_API_KEY`
env) does not have the `demos:read` scope granted. Server-side API keys created at
developers.faceit.com need the download scope explicitly enabled.

### How to fix

1. **New CDN URL**: Obtain the current FACEIT demo CDN domain. Check:
   - FACEIT developer documentation at developers.faceit.com
   - The `demo_url` field returned by the FACEIT Data API v4
     (`GET /data/v4/matches/{matchId}`) — it contains the full direct download URL
     for each match; no separate downloads API call is needed if this URL is accessible.
   - Community tooling such as cs-demo-manager or Leetify to see what CDN they use.

2. **API key scope**: Re-create the server-side FACEIT API key with `demo_download`
   scope (exact scope name may vary — check the developer portal). Or use the direct
   `demo_url` from the match data response, which may not require a special scope.

3. **Code path** (`cmd/fetch.go`):
   - `runFetch` → `doFetch` calls `internal/faceit/client.Client.RecentMatches` to
     get match metadata, then calls `downloadAndDecompress(demoURL, ...)` for each.
   - `downloadAndDecompress` handles both `.dem.gz` and `.dem.bz2` content.
   - Once a working `demo_url` is confirmed, the only change needed is pointing at
     the correct URL (likely already in the match metadata response).

---

## Path 2 — Valve MM / Premier Demos

### Current status: non-functional (Game Coordinator auth required)

### What works today

The share code chain enumeration works completely. `cmd/fetchmm.go` calls
`internal/steam/client.Client.NextShareCode` which correctly walks the Steam Web API
chain (`ICSGOPlayers_730/GetNextMatchSharingCode/v1`) and prints each match's share
code and expected demo filename (`{matchID}_{reservationID}_{tvPort}.dem.bz2`).

This lets you see your recent matches and know what filename to look for. Manual
download via CS2 Watch menu or third-party tools then feeds into
`csmetrics parse --dir <dir>` as normal.

### What is blocked

Valve's CS2 replay servers (`replay100.valve.net` through `replay228.valve.net`) return
HTTP 403 for **all** requests, regardless of path, credentials, or user-agent. They are
not publicly accessible. CS2 moved to signed URL tokens issued by the **Steam Game
Coordinator (GC)** — a Protobuf-over-TCP service that runs inside Steam client.

The GC response for a match info request (`CMsgGCCStrike15_v2_MatchInfo`) contains a
`map` field with the full signed download URL. This URL includes a time-limited token
and is only valid for ~30 days after the match.

### What the Steam Web API does NOT provide

The public `ICSGOPlayers_730` share code API only gives you the next share code in
the chain. It does **not** return the signed replay URL. There is no HTTP endpoint
for this — the GC is only reachable via the Steam client binary protocol.

### Implementation path

To automate Valve MM/Premier downloads, you need one of:

**Option A — Steam GC via go-steam**

[go-steam](https://github.com/Philipp15b/go-steam) implements the Steam client binary
protocol in Go, including GC message dispatch.

Relevant GC message type: `CMsgGCCStrike15_v2_MatchListRequestRecentUserGames` (type
`k_EMsgGCCStrike15_v2_MatchListRequestRecentUserGames = 9105`). The response is
`CMsgGCCStrike15_v2_MatchList`, which contains `CMsgGCCStrike15_v2_MatchInfo` entries,
each with a `map` (download URL) field.

Rough implementation sketch:

```go
// 1. Log in to Steam with username + password (+ 2FA code/guard).
client := steam.NewClient()
client.Connect()
// ... handle LoggedOnCallback

// 2. Send GC hello to CS2 (App ID 730).
client.GC.SetGamesPlayed(730)
// ... handle GCReadyCallback

// 3. Request match list.
req := &csgo.CMsgGCCStrike15_v2_MatchListRequestRecentUserGames{
    Accountid: proto.Uint32(steamid.SteamId.GetAccountId()),
}
client.GC.Write(steam.NewGameMessage(730, csgo.ECsgoGCMsg_k_EMsgGCCStrike15_v2_MatchListRequestRecentUserGames, req))
// ... handle GCMessageCallback for k_EMsgGCCStrike15_v2_MatchList

// 4. Extract download URLs from response.Matches[i].GetMap()
```

Protobuf definitions are available at:
- https://github.com/nicklvsa/go-csgoproto (Go)
- https://github.com/nicklvsa/csgo-protobufs (source)
- The Steam SDK (`steammessages_clientserver_login.proto`, `cstrike15_gcmessages.proto`)

**Caveats with go-steam / Steam GC approach:**
- Requires a real Steam account with CS2 owned.
- Requires the account to NOT be logged in elsewhere (same-account login kicks the
  prior session).
- Steam may flag scripted logins as suspicious; consider a dedicated secondary account.
- 2FA / Steam Guard codes must be handled (either TOTP via shared secret, or
  interactive prompt on first run).
- The go-steam library has not been maintained recently; Valve occasionally changes
  the Steam protocol. Check fork activity before investing.

**Option B — SteamKit2 (C#)**

[SteamKit2](https://github.com/SteamRE/SteamKit2) is the gold-standard Steam protocol
implementation in C#. cs-demo-manager (which does download Valve demos) uses this.

If implementing in Go is blocked by protocol-level issues, wrapping a small C# shim
that takes a share code and outputs the download URL (then calling it from Go via
`exec.Command`) is a pragmatic short-term path.

**Option C — GCPD scraping (fragile, no code needed)**

[CS2 GCPD](https://help.steampowered.com/en/wizard/HelpWithGameIssue/?appid=730&issueid=128)
is Steam's game coordinator player data page. It lists recent Premier and Competitive
matches with download links. Scraping this page with a session cookie provides signed
URLs without implementing the GC protocol.

This is fragile (HTML structure changes, session management, rate limiting) and should
only be considered as a last resort.

**Option D — Leverage existing tooling**

Tools that already solve the GC auth problem:

| Tool | Language | License | Notes |
|------|----------|---------|-------|
| [cs-demo-manager](https://github.com/akiver/cs-demo-manager) | Electron/TS | GPL-3 | Full GUI; can batch download demos |
| [cs2-demo-downloader](https://github.com/nicklvsa/cs2-demo-downloader) | Go | MIT | CLI; uses Steam GC via go-steam |
| Refrag | SaaS | Proprietary | Web platform; exports .dem files |
| Leetify | SaaS | Proprietary | Web platform; provides demo download links |

The simplest path to automated ingestion today:
1. Use `csmetrics fetch-mm` (once re-enabled or run directly) to enumerate share codes.
2. Use cs-demo-manager's batch download feature to download the corresponding demos.
3. Run `csmetrics parse --dir <download-folder>` to ingest them.

---

## Existing Code State

### `cmd/fetch.go` (FACEIT)

- `runFetch` / `doFetch`: fetch match list from FACEIT, download and parse each demo.
- `downloadAndDecompress`: HTTP download with `.dem.gz` and `.dem.bz2` support.
- Only needs: a working `demo_url` from the FACEIT match API response.
- **To re-enable**: fix CDN/scope issue, then add `rootCmd.AddCommand(fetchCmd)` in `cmd/root.go`.

### `cmd/fetchmm.go` (Valve MM)

- `doFetchMM`: walks the Steam share code chain and prints match info.
- Currently only enumerates — no download.
- `loadSteamAPIKey` / `loadMMLastCode` / `saveMMLastCode`: credential and state persistence.
- **To extend**: add a `downloadMatch(shareCode ShareCode, outDir string) error` function
  that calls the GC (via go-steam or equivalent), extracts the signed URL, and hands
  it to `downloadAndDecompress`.
- **To re-enable**: add `rootCmd.AddCommand(fetchMMCmd)` in `cmd/root.go`.

### `internal/steam/` package

- `sharecode.go`: Base-57 CS2 share code decoder (`Decode` → `ShareCode{MatchID, ReservationID, TVPort}`).
- `client.go`: Steam Web API client (`NextShareCode`, `DemoFilename`).
- Both are fully functional and tested against the live Steam API.
