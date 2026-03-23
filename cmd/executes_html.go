package cmd

import (
	"bytes"
	"compress/gzip"
	_ "embed"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/pable/go-cs-metrics/internal/model"
)

//go:embed executes_template.html
var executesTemplateHTML string

// Map coordinate metadata — same values as cs-demo-viewer/internal/maps/maps.go.
var executeMapMetas = map[string]execMapMeta{
	"de_ancient":  {PosX: -2953, PosY: 2164, Scale: 5.0},
	"de_anubis":   {PosX: -2796, PosY: 3328, Scale: 5.22},
	"de_dust2":    {PosX: -2476, PosY: 3239, Scale: 4.4},
	"de_inferno":  {PosX: -2087, PosY: 3870, Scale: 4.9},
	"de_mirage":   {PosX: -3230, PosY: 1713, Scale: 5.0},
	"de_nuke":     {PosX: -3453, PosY: 2887, Scale: 7.0},
	"de_overpass": {PosX: -4831, PosY: 1781, Scale: 5.2},
	"de_train":    {PosX: -2308, PosY: 2078, Scale: 4.082077},
	"de_vertigo":  {PosX: -3168, PosY: 1762, Scale: 4.0},
}

// Lower-level Z thresholds for multi-floor maps.
var executeMapLowers = map[string]float64{
	"de_nuke":    -495,
	"de_vertigo": 11700,
	"de_train":   -130,
}

// ── Injected data structures ──────────────────────────────────────────────────

type execClipData struct {
	Maps  map[string]execMapData `json:"maps"`
	Clips []execClip             `json:"clips"`
}

type execMapData struct {
	Meta      execMapMeta `json:"meta"`
	Radar     string      `json:"radar"`      // "data:image/png;base64,..."
	HasLower  bool        `json:"has_lower"`
	RadarLower string     `json:"radar_lower"` // "" if no lower
	LowerZMax  float64    `json:"lower_z_max"`
}

type execMapMeta struct {
	PosX  float64 `json:"pos_x"`
	PosY  float64 `json:"pos_y"`
	Scale float64 `json:"scale"`
}

type execClip struct {
	// Execute metadata
	Match   string `json:"match"`
	Date    string `json:"date"`
	Map     string `json:"map"`
	Rnd     int    `json:"rnd"`
	Site    string `json:"site"`
	Smokes  int    `json:"smokes"`
	Flashes int    `json:"flashes"`
	Mollies int    `json:"mollies"`
	TAlive  int    `json:"t_alive"`
	Defused bool   `json:"defused"`
	TWon    bool   `json:"t_won"`
	// Viewer data
	Tickrate  float64              `json:"tickrate"`
	Players   []execPlayer         `json:"players"`
	RoundMeta execRoundMeta        `json:"round_meta"`
	Frames    []model.UnifiedFrame `json:"frames"`
	Kills     []execKill           `json:"kills"`
	Grenades  []execGrenade        `json:"grenades"`
	Trails    []execTrail          `json:"trails"`
	Shots     []execShot           `json:"shots"`
	Bombs     []execBomb           `json:"bombs"`
}

type execPlayer struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type execRoundMeta struct {
	N       int    `json:"n"`
	W       string `json:"w"`
	CTS     int    `json:"cts"`
	TS      int    `json:"ts"`
	FE      int    `json:"fe"`
	EndTick int    `json:"end_tick"`
}

// Compact array types matching cs-demo-viewer's JSON format so the template
// can reuse the same rendering constants.

type execKill struct {
	Tick    int
	AtkIdx  int
	VicIdx  int
	Weapon  string
	HS      bool
	AtkX, AtkY int
	VicX, VicY int
	AsiIdx  int
	FA      bool
}

func (k execKill) MarshalJSON() ([]byte, error) {
	b := func(v bool) int {
		if v {
			return 1
		}
		return 0
	}
	return json.Marshal([]any{k.Tick, k.AtkIdx, k.VicIdx, k.Weapon, b(k.HS),
		k.AtkX, k.AtkY, k.VicX, k.VicY, k.AsiIdx, b(k.FA)})
}

type execGrenade struct {
	StartTick, EndTick int
	Type               int
	X, Y               int
	ThrowerIdx         int
}

func (g execGrenade) MarshalJSON() ([]byte, error) {
	return json.Marshal([6]int{g.StartTick, g.EndTick, g.Type, g.X, g.Y, g.ThrowerIdx})
}

type execTrail struct {
	StartTick, EndTick int
	Type               int
	ThrowerIdx         int
	Points             [][3]int
}

func (t execTrail) MarshalJSON() ([]byte, error) {
	return json.Marshal([]any{t.StartTick, t.EndTick, t.Type, t.ThrowerIdx, t.Points})
}

type execShot struct {
	Tick int
	Idx  int
}

func (s execShot) MarshalJSON() ([]byte, error) {
	return json.Marshal([2]int{s.Tick, s.Idx})
}

type execBomb struct {
	Tick   int
	Action int
	X, Y   int
	Site   string
}

func (b execBomb) MarshalJSON() ([]byte, error) {
	return json.Marshal([]any{b.Tick, b.Action, b.X, b.Y, b.Site})
}

// ── Main HTML generation ──────────────────────────────────────────────────────

func writeExecuteHTML(records []executeRecord, outPath, radarDir string) error {
	// Collect unique map names from records that have viewer data.
	mapNames := map[string]struct{}{}
	for _, r := range records {
		if r.viewerData != nil {
			mapNames[r.Map] = struct{}{}
		}
	}

	// Load radar PNGs for each map.
	mapDatas := make(map[string]execMapData, len(mapNames))
	for mapName := range mapNames {
		md := execMapData{}
		if meta, ok := executeMapMetas[mapName]; ok {
			md.Meta = meta
		}
		md.Radar = loadRadarPNG(radarDir, mapName)
		if zMax, ok := executeMapLowers[mapName]; ok {
			md.HasLower = true
			md.LowerZMax = zMax
			md.RadarLower = loadRadarPNG(radarDir, mapName+"_lower")
		}
		mapDatas[mapName] = md
	}

	// Build clips.
	clips := make([]execClip, 0, len(records))
	for _, r := range records {
		if r.viewerData == nil {
			continue
		}
		clips = append(clips, buildExecClip(r))
	}

	// Marshal to JSON, gzip, base64.
	data := execClipData{Maps: mapDatas, Clips: clips}
	jsonBytes, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("marshal clip data: %w", err)
	}

	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	if _, err := gz.Write(jsonBytes); err != nil {
		return fmt.Errorf("gzip: %w", err)
	}
	if err := gz.Close(); err != nil {
		return fmt.Errorf("gzip close: %w", err)
	}
	b64 := base64.StdEncoding.EncodeToString(buf.Bytes())

	// Inject into template.
	html := strings.Replace(executesTemplateHTML, "/*INJECT_DATA*/", b64, 1)

	f, err := os.Create(outPath)
	if err != nil {
		return fmt.Errorf("create %s: %w", outPath, err)
	}
	defer f.Close()
	_, err = f.WriteString(html)
	return err
}

// buildExecClip converts an executeRecord (with viewerData) to an execClip.
func buildExecClip(r executeRecord) execClip {
	vd := r.viewerData

	// Build SteamID → player index map.
	steamToIdx := make(map[uint64]int, len(vd.Players))
	for i, p := range vd.Players {
		steamToIdx[p.SteamID] = i
	}

	// Convert players.
	players := make([]execPlayer, len(vd.Players))
	for i, p := range vd.Players {
		players[i] = execPlayer{
			ID:   fmt.Sprintf("%d", p.SteamID),
			Name: p.Name,
		}
	}

	// Round metadata.
	winner := ""
	switch vd.RoundMeta.WinnerTeam {
	case model.TeamT:
		winner = "T"
	case model.TeamCT:
		winner = "CT"
	}
	roundMeta := execRoundMeta{
		N:       vd.RoundMeta.Number,
		W:       winner,
		CTS:     vd.RoundMeta.CTScore,
		TS:      vd.RoundMeta.TScore,
		FE:      vd.RoundMeta.FreezeEndTick,
		EndTick: vd.RoundMeta.EndTick,
	}

	// Convert kills (SteamID → player index).
	kills := make([]execKill, 0, len(vd.Kills))
	for _, k := range vd.Kills {
		atkIdx, aok := steamToIdx[k.KillerSteamID]
		vicIdx, vok := steamToIdx[k.VictimSteamID]
		if !aok || !vok {
			continue
		}
		asiIdx := -1
		if k.AssisterSteamID != 0 {
			if i, ok := steamToIdx[k.AssisterSteamID]; ok {
				asiIdx = i
			}
		}
		kills = append(kills, execKill{
			Tick:   k.Tick,
			AtkIdx: atkIdx,
			VicIdx: vicIdx,
			Weapon: k.Weapon,
			HS:     k.IsHeadshot,
			AtkX:   k.KillerX,
			AtkY:   k.KillerY,
			VicX:   k.VictimX,
			VicY:   k.VictimY,
			AsiIdx: asiIdx,
			FA:     k.AssistedFlash,
		})
	}

	// Convert grenades.
	grenades := make([]execGrenade, len(vd.Grenades))
	for i, g := range vd.Grenades {
		grenades[i] = execGrenade{
			StartTick:  g.StartTick,
			EndTick:    g.EndTick,
			Type:       g.Type,
			X:          g.X,
			Y:          g.Y,
			ThrowerIdx: g.ThrowerIdx,
		}
	}

	// Convert trails.
	trails := make([]execTrail, len(vd.Trails))
	for i, t := range vd.Trails {
		trails[i] = execTrail{
			StartTick:  t.StartTick,
			EndTick:    t.EndTick,
			Type:       t.Type,
			ThrowerIdx: t.ThrowerIdx,
			Points:     t.Points,
		}
	}

	// Convert shots.
	shots := make([]execShot, len(vd.Shots))
	for i, s := range vd.Shots {
		shots[i] = execShot{Tick: s.Tick, Idx: s.Idx}
	}

	// Convert bombs.
	bombs := make([]execBomb, len(vd.Bombs))
	for i, b := range vd.Bombs {
		bombs[i] = execBomb{
			Tick:   b.Tick,
			Action: b.Action,
			X:      b.X,
			Y:      b.Y,
			Site:   b.Site,
		}
	}

	return execClip{
		Match:     r.Match,
		Date:      r.Date,
		Map:       r.Map,
		Rnd:       r.Round,
		Site:      r.Site,
		Smokes:    r.Smokes,
		Flashes:   r.Flashes,
		Mollies:   r.Mollies,
		TAlive:    r.TAlive,
		Defused:   r.Defused,
		TWon:      r.TWon,
		Tickrate:  vd.Tickrate,
		Players:   players,
		RoundMeta: roundMeta,
		Frames:    vd.Frames,
		Kills:     kills,
		Grenades:  grenades,
		Trails:    trails,
		Shots:     shots,
		Bombs:     bombs,
	}
}

// loadRadarPNG reads a radar PNG from radarDir and returns a data URL string.
// Returns "" if the file cannot be read.
func loadRadarPNG(radarDir, mapName string) string {
	path := filepath.Join(radarDir, mapName+".png")
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(b)
}
