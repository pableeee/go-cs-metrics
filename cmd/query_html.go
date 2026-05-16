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

	"github.com/pable/go-cs-metrics/internal/roundquery"
)

//go:embed query_template.html
var queryTemplateHTML string

// Map coordinate metadata — same values as cs-demo-viewer/internal/maps/maps.go.
var queryMapMetas = map[string]queryMapMeta{
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
var queryMapLowers = map[string]float64{
	"de_nuke":    -495,
	"de_vertigo": 11700,
	"de_train":   -130,
}

// ── Injected data structures ──────────────────────────────────────────────────

type queryClipData struct {
	Maps  map[string]queryMapData `json:"maps"`
	Clips []queryClip             `json:"clips"`
}

type queryMapData struct {
	Meta       queryMapMeta `json:"meta"`
	Radar      string       `json:"radar"` // "data:image/png;base64,..."
	HasLower   bool         `json:"has_lower"`
	RadarLower string       `json:"radar_lower"` // "" if no lower
	LowerZMax  float64      `json:"lower_z_max"`
}

type queryMapMeta struct {
	PosX  float64 `json:"pos_x"`
	PosY  float64 `json:"pos_y"`
	Scale float64 `json:"scale"`
}

type queryClip struct {
	// Round identity
	Match  string `json:"match"`
	Date   string `json:"date"`
	Map    string `json:"map"`
	Rnd    int    `json:"rnd"`
	Winner string `json:"winner"` // "T" | "CT"
	// Buy types
	TypeT  string `json:"type_t"`
	TypeCT string `json:"type_ct"`
	// Utility summary (T side)
	SmokesT   int `json:"smokes_t"`
	FlashesT  int `json:"flashes_t"`
	MolotovsT int `json:"molotovs_t"`
	// Utility summary (CT side)
	SmokesCT   int `json:"smokes_ct"`
	FlashesCT  int `json:"flashes_ct"`
	MolotovsCT int `json:"molotovs_ct"`
	// Alive counts at plant
	AlivePlantT  int `json:"alive_plant_t"`
	AlivePlantCT int `json:"alive_plant_ct"`
	// Bomb events
	Planted  bool   `json:"planted"`
	Defused  bool   `json:"defused"`
	Exploded bool   `json:"exploded"`
	Entry    string `json:"entry"` // "T" | "CT" | ""
	// Viewer data
	Tickrate  float64        `json:"tickrate"`
	Players   []queryPlayer  `json:"players"`
	RoundMeta queryRoundMeta `json:"round_meta"`
	Frames    []queryFrame   `json:"frames"`
	Kills     []queryKill    `json:"kills"`
	Grenades  []queryGrenade `json:"grenades"`
	Trails    []queryTrail   `json:"trails"`
	Shots     []queryShot    `json:"shots"`
	Bombs     []queryBomb    `json:"bombs"`
}

type queryPlayer struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type queryRoundMeta struct {
	N       int    `json:"n"`
	W       string `json:"w"`
	CTS     int    `json:"cts"`
	TS      int    `json:"ts"`
	FE      int    `json:"fe"`
	EndTick int    `json:"end_tick"`
}

// queryFrame is one sampled tick worth of player states, serialised as
// `{tick, round, p:[[idx,flags,hp,x,y,z,yaw,weapon,utility,money], ...]}` to
// match cs-demo-viewer's expected JSON shape.
type queryFrame struct {
	Tick   int
	Round  int
	States []queryPlayerState
}

func (f queryFrame) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		Tick   int                `json:"tick"`
		Round  int                `json:"round"`
		States []queryPlayerState `json:"p"`
	}{Tick: f.Tick, Round: f.Round, States: f.States})
}

type queryPlayerState struct {
	Idx     int
	Flags   int
	HP      int
	X, Y, Z int
	Yaw     int
	Weapon  string
	Utility int
	Money   int
}

func (ps queryPlayerState) MarshalJSON() ([]byte, error) {
	return json.Marshal([]any{ps.Idx, ps.Flags, ps.HP, ps.X, ps.Y, ps.Z,
		ps.Yaw, ps.Weapon, ps.Utility, ps.Money})
}

// Compact array types matching cs-demo-viewer's JSON format.

type queryKill struct {
	Tick           int
	AtkIdx, VicIdx int
	Weapon         string
	HS             bool
	AtkX, AtkY     int
	VicX, VicY     int
	AsiIdx         int
	FA             bool
}

func (k queryKill) MarshalJSON() ([]byte, error) {
	b := func(v bool) int {
		if v {
			return 1
		}
		return 0
	}
	return json.Marshal([]any{k.Tick, k.AtkIdx, k.VicIdx, k.Weapon, b(k.HS),
		k.AtkX, k.AtkY, k.VicX, k.VicY, k.AsiIdx, b(k.FA)})
}

type queryGrenade struct {
	StartTick, EndTick int
	Type               int
	X, Y               int
	ThrowerIdx         int
}

func (g queryGrenade) MarshalJSON() ([]byte, error) {
	return json.Marshal([6]int{g.StartTick, g.EndTick, g.Type, g.X, g.Y, g.ThrowerIdx})
}

type queryTrail struct {
	StartTick, EndTick int
	Type               int
	ThrowerIdx         int
	Points             [][3]int
}

func (t queryTrail) MarshalJSON() ([]byte, error) {
	return json.Marshal([]any{t.StartTick, t.EndTick, t.Type, t.ThrowerIdx, t.Points})
}

type queryShot struct {
	Tick int
	Idx  int
}

func (s queryShot) MarshalJSON() ([]byte, error) {
	return json.Marshal([2]int{s.Tick, s.Idx})
}

type queryBomb struct {
	Tick   int
	Action int
	X, Y   int
	Site   string
}

func (b queryBomb) MarshalJSON() ([]byte, error) {
	return json.Marshal([]any{b.Tick, b.Action, b.X, b.Y, b.Site})
}

// ── Main HTML generation ──────────────────────────────────────────────────────

func writeQueryHTML(records []roundquery.RoundRecord, outPath, radarDir string) error {
	// Collect unique map names from records that have viewer data.
	mapNames := map[string]struct{}{}
	for _, r := range records {
		if r.ViewerData != nil {
			mapNames[r.Map] = struct{}{}
		}
	}

	// Load radar PNGs for each map.
	mapDatas := make(map[string]queryMapData, len(mapNames))
	for mapName := range mapNames {
		md := queryMapData{}
		if meta, ok := queryMapMetas[mapName]; ok {
			md.Meta = meta
		}
		md.Radar = loadQueryRadarPNG(radarDir, mapName)
		if zMax, ok := queryMapLowers[mapName]; ok {
			md.HasLower = true
			md.LowerZMax = zMax
			md.RadarLower = loadQueryRadarPNG(radarDir, mapName+"_lower")
		}
		mapDatas[mapName] = md
	}

	// Build clips.
	clips := make([]queryClip, 0, len(records))
	for _, r := range records {
		if r.ViewerData == nil {
			continue
		}
		clips = append(clips, buildQueryClip(r))
	}

	// Marshal to JSON, gzip, base64.
	data := queryClipData{Maps: mapDatas, Clips: clips}
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
	html := strings.Replace(queryTemplateHTML, "/*INJECT_DATA*/", b64, 1)

	f, err := os.Create(outPath)
	if err != nil {
		return fmt.Errorf("create %s: %w", outPath, err)
	}
	defer f.Close()
	_, err = f.WriteString(html)
	return err
}

// buildQueryClip converts a RoundRecord (with ViewerData) to a queryClip.
func buildQueryClip(r roundquery.RoundRecord) queryClip {
	vd := r.ViewerData

	// Build player list in csraw2 slot order; slot is the index used by all
	// other arrays.
	players := make([]queryPlayer, len(vd.Players))
	for i, p := range vd.Players {
		players[i] = queryPlayer{
			ID:   fmt.Sprintf("%d", p.SteamID),
			Name: p.Name,
		}
	}

	roundMeta := queryRoundMeta{
		N:       vd.RoundMeta.Number,
		W:       vd.RoundMeta.WinnerTeam,
		CTS:     vd.RoundMeta.CTScore,
		TS:      vd.RoundMeta.TScore,
		FE:      vd.RoundMeta.FreezeEndTick,
		EndTick: vd.RoundMeta.EndTick,
	}

	// Frames.
	frames := make([]queryFrame, len(vd.Frames))
	for i, f := range vd.Frames {
		states := make([]queryPlayerState, len(f.States))
		for j, s := range f.States {
			states[j] = queryPlayerState{
				Idx:     int(s.Slot),
				Flags:   s.Flags,
				HP:      s.HP,
				X:       s.X,
				Y:       s.Y,
				Z:       s.Z,
				Yaw:     s.Yaw,
				Weapon:  s.Weapon,
				Utility: s.Utility,
				Money:   s.Money,
			}
		}
		frames[i] = queryFrame{Tick: f.Tick, Round: vd.RoundMeta.Number, States: states}
	}

	// Kills.
	kills := make([]queryKill, 0, len(vd.Kills))
	for _, k := range vd.Kills {
		if k.KillerSlot < 0 {
			continue
		}
		asiIdx := -1
		if k.AssisterSlot >= 0 {
			asiIdx = int(k.AssisterSlot)
		}
		kills = append(kills, queryKill{
			Tick:   k.Tick,
			AtkIdx: int(k.KillerSlot),
			VicIdx: int(k.VictimSlot),
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

	// Grenades.
	grenades := make([]queryGrenade, len(vd.Grenades))
	for i, g := range vd.Grenades {
		grenades[i] = queryGrenade{
			StartTick:  g.StartTick,
			EndTick:    g.EndTick,
			Type:       g.Type,
			X:          g.X,
			Y:          g.Y,
			ThrowerIdx: g.ThrowerIdx,
		}
	}

	// Trails.
	trails := make([]queryTrail, len(vd.Trails))
	for i, t := range vd.Trails {
		trails[i] = queryTrail{
			StartTick:  t.StartTick,
			EndTick:    t.EndTick,
			Type:       t.Type,
			ThrowerIdx: t.ThrowerIdx,
			Points:     t.Points,
		}
	}

	// Shots.
	shots := make([]queryShot, len(vd.Shots))
	for i, s := range vd.Shots {
		shots[i] = queryShot{Tick: s.Tick, Idx: int(s.Slot)}
	}

	// Bombs.
	bombs := make([]queryBomb, len(vd.Bombs))
	for i, b := range vd.Bombs {
		bombs[i] = queryBomb{
			Tick:   b.Tick,
			Action: b.Action,
			X:      b.X,
			Y:      b.Y,
			Site:   b.Site,
		}
	}

	return queryClip{
		Match:        r.MatchFile,
		Date:         r.Date,
		Map:          r.Map,
		Rnd:          r.RoundNum,
		Winner:       r.Winner,
		TypeT:        r.TypeT,
		TypeCT:       r.TypeCT,
		SmokesT:      r.SmokesT,
		FlashesT:     r.FlashesT,
		MolotovsT:    r.MolotovsT,
		SmokesCT:     r.SmokesCT,
		FlashesCT:    r.FlashesCT,
		MolotovsCT:   r.MolotovsCT,
		AlivePlantT:  r.AlivePlantT,
		AlivePlantCT: r.AlivePlantCT,
		Planted:      r.Planted,
		Defused:      r.Defused,
		Exploded:     r.Exploded,
		Entry:        r.EntrySide,
		Tickrate:     vd.Tickrate,
		Players:      players,
		RoundMeta:    roundMeta,
		Frames:       frames,
		Kills:        kills,
		Grenades:     grenades,
		Trails:       trails,
		Shots:        shots,
		Bombs:        bombs,
	}
}

// loadQueryRadarPNG reads a radar PNG from radarDir and returns a data URL string.
// Returns "" if the file cannot be read.
func loadQueryRadarPNG(radarDir, mapName string) string {
	path := filepath.Join(radarDir, mapName+".png")
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(b)
}
