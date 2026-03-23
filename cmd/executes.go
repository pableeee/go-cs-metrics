package cmd

import (
	"encoding/csv"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/pable/go-cs-metrics/internal/model"
)

var (
	executesDir       string
	executesMinSmokes int
	executesMinAlive  int
	executesSite      string
	executesMap       string
	executesCSV       bool
	executesHTML      string
	executesLimit     int
	executesRadarDir  string
)

var executesCmd = &cobra.Command{
	Use:   "executes --dir <dir>",
	Short: "Detect T-side site executes from .csdem.gz files",
	Long: `Scan .csdem.gz files for rounds where Ts executed onto a bombsite.

An execute is defined as a planted-bomb round where:
  - At least --min-smokes T-side smokes landed before the plant
  - At least --min-alive T players were alive at plant time

Walks --dir recursively, so you can point it at the full converted-pro directory.

Output columns:
  DATE, MAP, MATCH, RND, SITE, SMK, FLA, MOL, T_ALIVE, OUTCOME, T_WON

Examples:
  go-cs-metrics executes --dir ~/demos/converted-pro/
  go-cs-metrics executes --dir ~/demos/converted-pro/iem_cologne_2025/ --site A
  go-cs-metrics executes --dir ~/demos/converted-pro/ --min-smokes 3 --csv > out.csv
  go-cs-metrics executes --dir ~/demos/converted-pro/ --html executes.html --limit 100`,
	RunE: runExecutes,
}

func init() {
	home, _ := os.UserHomeDir()
	defaultRadarDir := filepath.Join(home, "git", "cs", "cs-demo-viewer", "internal", "maps", "overviews")

	executesCmd.Flags().StringVar(&executesDir, "dir", "", "root directory to scan for .csdem.gz files (required)")
	executesCmd.Flags().IntVar(&executesMinSmokes, "min-smokes", 2, "minimum T smokes before plant to qualify")
	executesCmd.Flags().IntVar(&executesMinAlive, "min-alive", 3, "minimum T players alive at plant to qualify")
	executesCmd.Flags().StringVar(&executesSite, "site", "", `filter by bombsite: "A" or "B" (default: both)`)
	executesCmd.Flags().StringVar(&executesMap, "map", "", "filter by map name (e.g. de_mirage)")
	executesCmd.Flags().BoolVar(&executesCSV, "csv", false, "output CSV instead of text table")
	executesCmd.Flags().StringVar(&executesHTML, "html", "", "write a 2D visual replay HTML file for all detected executes")
	executesCmd.Flags().IntVar(&executesLimit, "limit", 100, "maximum number of execute clips to include in the HTML output")
	executesCmd.Flags().StringVar(&executesRadarDir, "radar-dir", defaultRadarDir, "directory containing map radar PNGs (used with --html)")
	executesCmd.MarkFlagRequired("dir")
}

// executeRecord holds data for one detected execute round.
type executeRecord struct {
	Date    string
	Map     string
	Match   string // filename without extension
	Round   int
	Site    string
	Smokes  int
	Flashes int
	Mollies int
	TAlive  int
	Defused bool
	TWon    bool
	// Populated when --html is set:
	viewerData *executeViewerData
}

// executeViewerData holds the raw unified data needed to render the round.
type executeViewerData struct {
	Players   []model.UnifiedPlayer
	RoundMeta model.UnifiedRound
	Tickrate  float64
	Frames    []model.UnifiedFrame
	Kills     []model.UnifiedKill
	Grenades  []model.UnifiedGrenade
	Trails    []model.UnifiedGrenadeTrail
	Shots     []model.UnifiedShot
	Bombs     []model.UnifiedBombAction
}

func runExecutes(cmd *cobra.Command, args []string) error {
	collectData := executesHTML != ""

	// Collect all .csdem.gz paths recursively.
	var paths []string
	err := filepath.WalkDir(executesDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && strings.HasSuffix(d.Name(), ".csdem.gz") {
			paths = append(paths, path)
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("walk %s: %w", executesDir, err)
	}
	if len(paths) == 0 {
		return fmt.Errorf("no .csdem.gz files found under %s", executesDir)
	}

	fmt.Fprintf(os.Stderr, "Scanning %d files...\n", len(paths))

	var records []executeRecord
	var scanErrors int

	for _, path := range paths {
		uf, err := model.LoadUnifiedMatchFile(path)
		if err != nil {
			fmt.Fprintf(os.Stderr, "  skip %s: %v\n", filepath.Base(path), err)
			scanErrors++
			continue
		}
		m := uf.Match

		if executesMap != "" && m.MapName != executesMap {
			continue
		}

		recs := detectExecutes(m, path, collectData)

		// When generating HTML, stop collecting data once we hit the limit.
		if collectData {
			remaining := executesLimit - len(records)
			if remaining <= 0 {
				// Still detect for count, but don't store viewer data.
				for i := range recs {
					recs[i].viewerData = nil
				}
			} else if remaining < len(recs) {
				for i := remaining; i < len(recs); i++ {
					recs[i].viewerData = nil
				}
			}
		}

		records = append(records, recs...)
	}

	fmt.Fprintf(os.Stderr, "Done. %d executes across %d files (%d errors).\n\n",
		len(records), len(paths), scanErrors)

	if len(records) == 0 {
		fmt.Println("No executes found matching the criteria.")
		return nil
	}

	// Write HTML if requested.
	if executesHTML != "" {
		clipped := records
		if len(clipped) > executesLimit {
			clipped = clipped[:executesLimit]
		}
		if err := writeExecuteHTML(clipped, executesHTML, executesRadarDir); err != nil {
			return fmt.Errorf("write HTML: %w", err)
		}
		fmt.Fprintf(os.Stderr, "HTML written to %s (%d clips)\n", executesHTML, len(clipped))
	}

	if executesCSV {
		printExecutesCSV(records)
	} else if executesHTML == "" {
		printExecutesTable(records)
	}
	return nil
}

// detectExecutes finds all qualifying T execute rounds in a single match.
// When collectData is true, each returned record's viewerData is populated.
func detectExecutes(m *model.UnifiedMatch, path string, collectData bool) []executeRecord {
	matchName := strings.TrimSuffix(filepath.Base(path), ".csdem.gz")

	// Build player idx → SteamID slice (idx is position in Match.Players).
	idxToSteam := make([]uint64, len(m.Players))
	for i, p := range m.Players {
		idxToSteam[i] = p.SteamID
	}

	// Index grenades, bombs, and frames by round number.
	grenadesByRound := make(map[int][]model.UnifiedGrenade, len(m.Rounds))
	for _, g := range m.Grenades {
		grenadesByRound[g.Round] = append(grenadesByRound[g.Round], g)
	}
	bombsByRound := make(map[int][]model.UnifiedBombAction, len(m.Rounds))
	for _, b := range m.Bombs {
		bombsByRound[b.Round] = append(bombsByRound[b.Round], b)
	}
	framesByRound := make(map[int][]model.UnifiedFrame, len(m.Rounds))
	for _, f := range m.Frames {
		framesByRound[f.Round] = append(framesByRound[f.Round], f)
	}

	// Build kills/trails/shots by round only when needed for HTML.
	var killsByRound map[int][]model.UnifiedKill
	var trailsByRound map[int][]model.UnifiedGrenadeTrail
	var shotsByRound map[int][]model.UnifiedShot
	if collectData {
		killsByRound = make(map[int][]model.UnifiedKill)
		for _, k := range m.Kills {
			killsByRound[k.Round] = append(killsByRound[k.Round], k)
		}
		trailsByRound = make(map[int][]model.UnifiedGrenadeTrail)
		for _, t := range m.Trails {
			trailsByRound[t.Round] = append(trailsByRound[t.Round], t)
		}
		shotsByRound = make(map[int][]model.UnifiedShot)
		for _, s := range m.Shots {
			shotsByRound[s.Round] = append(shotsByRound[s.Round], s)
		}
	}

	var records []executeRecord

	for _, round := range m.Rounds {
		if round.BombPlantTick == 0 {
			continue
		}

		plantTick, site := bombPlantInfo(bombsByRound[round.Number])
		if plantTick == 0 || site == "" {
			continue
		}
		if executesSite != "" && !strings.EqualFold(site, executesSite) {
			continue
		}

		// Build player idx → team from this round's PlayerEndState.
		idxTeam := make(map[int]model.Team, len(round.PlayerEndState))
		for i, steamID := range idxToSteam {
			if state, ok := round.PlayerEndState[steamID]; ok {
				idxTeam[i] = state.Team
			}
		}

		// Count T smokes, flashes, and molotovs that landed before plant.
		var smokes, flashes, mollies int
		for _, g := range grenadesByRound[round.Number] {
			if g.StartTick > plantTick {
				continue
			}
			throwerTeam := model.TeamUnknown
			if g.ThrowerIdx >= 0 && g.ThrowerIdx < len(idxTeam) {
				throwerTeam = idxTeam[g.ThrowerIdx]
			}
			switch g.Type {
			case 5: // T-smoke (tagged by team in converter)
				smokes++
			case 4: // CT-smoke — skip
			case 1: // flash
				if throwerTeam == model.TeamT {
					flashes++
				}
			case 3: // molotov / incendiary
				if throwerTeam == model.TeamT {
					mollies++
				}
			}
		}

		if smokes < executesMinSmokes {
			continue
		}

		tAlive := countTAliveAtTick(framesByRound[round.Number], plantTick)
		if tAlive < executesMinAlive {
			continue
		}

		defused := false
		for _, b := range bombsByRound[round.Number] {
			if b.Action == 3 {
				defused = true
				break
			}
		}

		rec := executeRecord{
			Date:    m.MatchDate,
			Map:     m.MapName,
			Match:   matchName,
			Round:   round.Number,
			Site:    site,
			Smokes:  smokes,
			Flashes: flashes,
			Mollies: mollies,
			TAlive:  tAlive,
			Defused: defused,
			TWon:    round.WinnerTeam == model.TeamT,
		}

		if collectData {
			rec.viewerData = &executeViewerData{
				Players:   m.Players,
				RoundMeta: round,
				Tickrate:  m.Tickrate,
				Frames:    framesByRound[round.Number],
				Kills:     killsByRound[round.Number],
				Grenades:  grenadesByRound[round.Number],
				Trails:    trailsByRound[round.Number],
				Shots:     shotsByRound[round.Number],
				Bombs:     bombsByRound[round.Number],
			}
		}

		records = append(records, rec)
	}

	return records
}

// bombPlantInfo returns the in-game tick and site label ("A"/"B") of the
// BombPlanted action (action==1) in the given bomb event list.
func bombPlantInfo(bombs []model.UnifiedBombAction) (tick int, site string) {
	for _, b := range bombs {
		if b.Action == 1 {
			return b.Tick, b.Site
		}
	}
	return 0, ""
}

// countTAliveAtTick returns the number of T-side alive players in the
// position frame at or just before targetTick.
// flags bits: 0=dead, 1=T(vs CT) → T+alive == (flags & 3) == 2
func countTAliveAtTick(frames []model.UnifiedFrame, targetTick int) int {
	best := -1
	for i, f := range frames {
		if f.Tick <= targetTick {
			best = i
		} else {
			break
		}
	}
	if best < 0 {
		return 0
	}
	count := 0
	for _, s := range frames[best].States {
		if (s.Flags & 3) == 2 {
			count++
		}
	}
	return count
}

func printExecutesTable(records []executeRecord) {
	fmt.Printf("%-10s  %-12s  %-42s  %3s  %-4s  %3s  %3s  %3s  %-6s  %-8s  %5s\n",
		"DATE", "MAP", "MATCH", "RND", "SITE", "SMK", "FLA", "MOL", "T_ALIVE", "OUTCOME", "T_WON")
	fmt.Printf("%-10s  %-12s  %-42s  %3s  %-4s  %3s  %3s  %3s  %-6s  %-8s  %5s\n",
		"──────────", "────────────",
		"──────────────────────────────────────────", "───", "────",
		"───", "───", "───", "──────", "────────", "─────")
	for _, r := range records {
		name := r.Match
		if len(name) > 42 {
			name = name[:39] + "..."
		}
		outcome := "exploded"
		if r.Defused {
			outcome = "defused"
		}
		won := "no"
		if r.TWon {
			won = "yes"
		}
		fmt.Printf("%-10s  %-12s  %-42s  %3d  %-4s  %3d  %3d  %3d  %-6d  %-8s  %5s\n",
			r.Date, r.Map, name, r.Round, r.Site,
			r.Smokes, r.Flashes, r.Mollies, r.TAlive,
			outcome, won)
	}
}

func printExecutesCSV(records []executeRecord) {
	w := csv.NewWriter(os.Stdout)
	_ = w.Write([]string{"date", "map", "match", "round", "site", "smokes", "flashes", "mollies", "t_alive", "defused", "t_won"})
	for _, r := range records {
		_ = w.Write([]string{
			r.Date, r.Map, r.Match,
			fmt.Sprint(r.Round), r.Site,
			fmt.Sprint(r.Smokes), fmt.Sprint(r.Flashes), fmt.Sprint(r.Mollies),
			fmt.Sprint(r.TAlive),
			fmt.Sprint(r.Defused), fmt.Sprint(r.TWon),
		})
	}
	w.Flush()
}
