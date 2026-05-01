// csraw2-compare runs the aggregator twice on the same .dem — once via
// the direct parser → RawMatch path, once via parserv2 → csraw2.Match →
// csraw2bridge → RawMatch — and reports any divergence in the produced
// metrics. Used to validate that v2 + bridge produces identical output
// to the v1 path, so we can swap them without losing fidelity.
//
// Usage:
//
//	csraw2-compare <demo.dem>                    # detailed table for one demo
//	csraw2-compare -batch <dir/*.dem>            # one-line summary per demo + aggregate
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"runtime/debug"
	"sort"
	"time"

	"github.com/pable/go-cs-metrics/internal/aggregator"
	"github.com/pable/go-cs-metrics/internal/csraw2bridge"
	"github.com/pable/go-cs-metrics/internal/model"
	"github.com/pable/go-cs-metrics/internal/parser"
	"github.com/pable/go-cs-metrics/internal/parserv2"
)

func main() {
	batch := flag.Bool("batch", false, "process every demo arg, print one summary line each + final aggregate")
	flag.Parse()

	demos := flag.Args()
	if len(demos) == 0 {
		fmt.Fprintln(os.Stderr, "usage: csraw2-compare [-batch] <demo.dem> [more.dem ...]")
		os.Exit(2)
	}

	if *batch {
		runBatch(demos)
		return
	}
	if len(demos) > 1 {
		fmt.Fprintln(os.Stderr, "multiple demos given without -batch; pass -batch to enable summary mode")
		os.Exit(2)
	}
	runSingleDetailed(demos[0])
}

func runSingleDetailed(path string) {

	// V1 path: parser → aggregator.
	t0 := time.Now()
	rawV1, err := parser.ParseDemo(path, "competitive")
	if err != nil {
		fmt.Fprintf(os.Stderr, "v1 parse: %v\n", err)
		os.Exit(1)
	}
	v1ParseTime := time.Since(t0)

	t1 := time.Now()
	msV1, rsV1, wsV1, dsV1, deV1, feV1, err := aggregator.Aggregate(rawV1)
	if err != nil {
		fmt.Fprintf(os.Stderr, "v1 aggregate: %v\n", err)
		os.Exit(1)
	}
	v1AggTime := time.Since(t1)

	// V2 path: parserv2 → bridge → aggregator.
	t2 := time.Now()
	matchV2, err := parserv2.ParseDemoV2(path, "personal", "competitive")
	if err != nil {
		fmt.Fprintf(os.Stderr, "v2 parse: %v\n", err)
		os.Exit(1)
	}
	v2ParseTime := time.Since(t2)

	t3 := time.Now()
	rawV2, err := csraw2bridge.ToRawMatch(matchV2)
	if err != nil {
		fmt.Fprintf(os.Stderr, "v2 bridge: %v\n", err)
		os.Exit(1)
	}
	v2BridgeTime := time.Since(t3)

	t4 := time.Now()
	msV2, rsV2, wsV2, dsV2, deV2, feV2, err := aggregator.Aggregate(rawV2)
	if err != nil {
		fmt.Fprintf(os.Stderr, "v2 aggregate: %v\n", err)
		os.Exit(1)
	}
	v2AggTime := time.Since(t4)

	fmt.Printf("v1 path: parse %s, aggregate %s\n",
		v1ParseTime.Round(10*time.Millisecond), v1AggTime.Round(time.Millisecond))
	fmt.Printf("v2 path: parse %s, bridge %s, aggregate %s\n",
		v2ParseTime.Round(10*time.Millisecond),
		v2BridgeTime.Round(time.Millisecond),
		v2AggTime.Round(time.Millisecond))
	fmt.Println()

	fmt.Println("counts:")
	fmt.Printf("  match_stats:  v1=%d v2=%d\n", len(msV1), len(msV2))
	fmt.Printf("  round_stats:  v1=%d v2=%d\n", len(rsV1), len(rsV2))
	fmt.Printf("  weapon_stats: v1=%d v2=%d\n", len(wsV1), len(wsV2))
	fmt.Printf("  duel_segs:    v1=%d v2=%d\n", len(dsV1), len(dsV2))
	fmt.Printf("  death_events: v1=%d v2=%d\n", len(deV1), len(deV2))
	fmt.Printf("  flash_events: v1=%d v2=%d\n", len(feV1), len(feV2))
	fmt.Println()

	// Per-player kill/death/damage/ADR comparison.
	mapByPlayer := func(stats []model.PlayerMatchStats) map[uint64]model.PlayerMatchStats {
		m := map[uint64]model.PlayerMatchStats{}
		for _, s := range stats {
			m[s.SteamID] = s
		}
		return m
	}
	v1Stats := mapByPlayer(msV1)
	v2Stats := mapByPlayer(msV2)

	ids := make([]uint64, 0, len(v1Stats))
	for id := range v1Stats {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return v1Stats[ids[i]].Name < v1Stats[ids[j]].Name })

	fmt.Printf("%-20s %12s %12s %12s %12s %12s\n", "player", "K_v1/v2", "D_v1/v2", "DMG_v1/v2", "ADR_v1/v2", "RDS_v1/v2")
	mismatches := 0
	for _, id := range ids {
		v1 := v1Stats[id]
		v2, ok := v2Stats[id]
		mark := ""
		if !ok {
			mark = " ✗ missing v2"
			mismatches++
			fmt.Printf("%-20s%s\n", trunc(v1.Name, 20), mark)
			continue
		}
		// Only flag mismatches on integer fields; floats can be very slightly
		// different from sample-vs-event derivation.
		if v1.Kills != v2.Kills || v1.Deaths != v2.Deaths ||
			v1.TotalDamage != v2.TotalDamage || v1.RoundsPlayed != v2.RoundsPlayed {
			mark = "  *"
			mismatches++
		}
		fmt.Printf("%-20s %5d/%-6d %5d/%-6d %5d/%-6d %5.1f/%-5.1f %5d/%-6d%s\n",
			trunc(v1.Name, 20),
			v1.Kills, v2.Kills,
			v1.Deaths, v2.Deaths,
			v1.TotalDamage, v2.TotalDamage,
			v1.ADR(), v2.ADR(),
			v1.RoundsPlayed, v2.RoundsPlayed,
			mark,
		)
	}
	fmt.Println()

	if mismatches > 0 {
		fmt.Printf("⚠ %d player rows diverged\n", mismatches)
	} else {
		fmt.Println("✓ all player rows match on integer fields")
	}

	// Cross-check Round counts.
	if len(rawV1.Rounds) != len(rawV2.Rounds) {
		fmt.Printf("⚠ round count diverged: v1=%d v2=%d\n", len(rawV1.Rounds), len(rawV2.Rounds))
	}
	if len(rawV1.Kills) != len(rawV2.Kills) {
		fmt.Printf("⚠ kill count diverged: v1=%d v2=%d\n", len(rawV1.Kills), len(rawV2.Kills))
	}
	if len(rawV1.Damages) != len(rawV2.Damages) {
		fmt.Printf("⚠ damage count diverged: v1=%d v2=%d\n", len(rawV1.Damages), len(rawV2.Damages))
	}
	if len(rawV1.WeaponFires) != len(rawV2.WeaponFires) {
		fmt.Printf("⚠ weapon_fire count diverged: v1=%d v2=%d\n", len(rawV1.WeaponFires), len(rawV2.WeaponFires))
	}

	// Spot-check the first kill row, raw, to surface schema mismatches early.
	if len(rawV1.Kills) > 0 && len(rawV2.Kills) > 0 {
		k1, k2 := rawV1.Kills[0], rawV2.Kills[0]
		if !reflect.DeepEqual(k1.KillerSteamID, k2.KillerSteamID) ||
			k1.Tick != k2.Tick || k1.Weapon != k2.Weapon {
			fmt.Println()
			fmt.Println("first-kill divergence:")
			fmt.Printf("  v1: %+v\n", k1)
			fmt.Printf("  v2: %+v\n", k2)
		}
	}
}

func trunc(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}

// ── Batch mode ─────────────────────────────────────────────────────────────

type batchResult struct {
	demo   string
	rounds int

	// Counts (v1 vs v2). All "ok" fields true mean fully matching.
	matchOK, roundOK, weaponOK, duelOK, deathOK, flashOK   bool
	killsOK, dmgsOK, firesOK                               bool
	playersDiverged                                        int
	roundsV1, roundsV2                                     int
	weaponV1, weaponV2, duelV1, duelV2, deathV1, deathV2   int
	flashV1, flashV2                                       int
	killV1, killV2, dmgV1, dmgV2, fireV1, fireV2           int
	parseErr                                               string
}

func runBatch(demos []string) {
	results := make([]batchResult, 0, len(demos))
	t0 := time.Now()
	for i, d := range demos {
		fmt.Printf("[%2d/%d] %-60s ", i+1, len(demos), trunc(filepath.Base(d), 60))
		r := runOne(d)
		results = append(results, r)
		printOneLine(r)

		// Free OS memory between demos — same hygiene the v1 batch parser
		// relies on. Without this, RSS climbs by a few GB per demo until
		// WSL OOM-kills us around demo 12.
		debug.FreeOSMemory()
		runtime.GC()
	}
	dur := time.Since(t0)

	fmt.Println()
	fmt.Printf("processed %d demos in %s\n", len(results), dur.Round(time.Second))
	printAggregate(results)
}

func runOne(path string) batchResult {
	r := batchResult{demo: filepath.Base(path)}

	rawV1, err := parser.ParseDemo(path, "competitive")
	if err != nil {
		r.parseErr = "v1: " + err.Error()
		return r
	}
	msV1, _, wsV1, dsV1, deV1, feV1, err := aggregator.Aggregate(rawV1)
	if err != nil {
		r.parseErr = "v1 agg: " + err.Error()
		return r
	}

	matchV2, err := parserv2.ParseDemoV2(path, "personal", "competitive")
	if err != nil {
		r.parseErr = "v2: " + err.Error()
		return r
	}
	rawV2, err := csraw2bridge.ToRawMatch(matchV2)
	if err != nil {
		r.parseErr = "bridge: " + err.Error()
		return r
	}
	msV2, _, wsV2, dsV2, deV2, feV2, err := aggregator.Aggregate(rawV2)
	if err != nil {
		r.parseErr = "v2 agg: " + err.Error()
		return r
	}

	// Counts
	r.rounds = len(rawV1.Rounds)
	r.roundsV1, r.roundsV2 = len(rawV1.Rounds), len(rawV2.Rounds)
	r.weaponV1, r.weaponV2 = len(wsV1), len(wsV2)
	r.duelV1, r.duelV2 = len(dsV1), len(dsV2)
	r.deathV1, r.deathV2 = len(deV1), len(deV2)
	r.flashV1, r.flashV2 = len(feV1), len(feV2)
	r.killV1, r.killV2 = len(rawV1.Kills), len(rawV2.Kills)
	r.dmgV1, r.dmgV2 = len(rawV1.Damages), len(rawV2.Damages)
	r.fireV1, r.fireV2 = len(rawV1.WeaponFires), len(rawV2.WeaponFires)

	r.matchOK = len(msV1) == len(msV2)
	r.roundOK = r.roundsV1 == r.roundsV2
	r.weaponOK = r.weaponV1 == r.weaponV2
	r.duelOK = r.duelV1 == r.duelV2
	r.deathOK = r.deathV1 == r.deathV2
	r.flashOK = r.flashV1 == r.flashV2
	r.killsOK = r.killV1 == r.killV2
	r.dmgsOK = r.dmgV1 == r.dmgV2
	r.firesOK = r.fireV1 == r.fireV2

	// Per-player divergences on integer fields.
	v1ByID := map[uint64]model.PlayerMatchStats{}
	for _, s := range msV1 {
		v1ByID[s.SteamID] = s
	}
	for _, s2 := range msV2 {
		s1, ok := v1ByID[s2.SteamID]
		if !ok {
			r.playersDiverged++
			continue
		}
		if s1.Kills != s2.Kills || s1.Deaths != s2.Deaths ||
			s1.TotalDamage != s2.TotalDamage || s1.RoundsPlayed != s2.RoundsPlayed {
			r.playersDiverged++
		}
	}
	return r
}

func printOneLine(r batchResult) {
	if r.parseErr != "" {
		fmt.Printf("✗ %s\n", r.parseErr)
		return
	}
	flags := ""
	if r.playersDiverged > 0 {
		flags += fmt.Sprintf(" plyrs=%d", r.playersDiverged)
	}
	if !r.killsOK {
		flags += fmt.Sprintf(" kills=%d/%d", r.killV1, r.killV2)
	}
	if !r.dmgsOK {
		flags += fmt.Sprintf(" dmg=%d/%d", r.dmgV1, r.dmgV2)
	}
	if !r.firesOK {
		flags += fmt.Sprintf(" fires=%d/%d", r.fireV1, r.fireV2)
	}
	if !r.duelOK {
		flags += fmt.Sprintf(" duel=%d/%d", r.duelV1, r.duelV2)
	}
	if !r.weaponOK {
		flags += fmt.Sprintf(" wpn=%d/%d", r.weaponV1, r.weaponV2)
	}
	if !r.deathOK {
		flags += fmt.Sprintf(" deaths=%d/%d", r.deathV1, r.deathV2)
	}
	if !r.flashOK {
		flags += fmt.Sprintf(" flash=%d/%d", r.flashV1, r.flashV2)
	}
	if flags == "" {
		fmt.Printf("✓ rounds=%d\n", r.rounds)
	} else {
		fmt.Printf("⚠ rounds=%d%s\n", r.rounds, flags)
	}
}

func printAggregate(results []batchResult) {
	var (
		errs, perfect, hasDiverg int
		anyPlayerDiv             int
		duelDiv, killDiv, dmgDiv int
		fireDiv, weaponDiv       int
		deathDiv, flashDiv       int
	)
	for _, r := range results {
		if r.parseErr != "" {
			errs++
			continue
		}
		clean := r.killsOK && r.dmgsOK && r.firesOK && r.duelOK &&
			r.weaponOK && r.deathOK && r.flashOK && r.playersDiverged == 0
		if clean {
			perfect++
			continue
		}
		hasDiverg++
		if r.playersDiverged > 0 {
			anyPlayerDiv++
		}
		if !r.duelOK {
			duelDiv++
		}
		if !r.killsOK {
			killDiv++
		}
		if !r.dmgsOK {
			dmgDiv++
		}
		if !r.firesOK {
			fireDiv++
		}
		if !r.weaponOK {
			weaponDiv++
		}
		if !r.deathOK {
			deathDiv++
		}
		if !r.flashOK {
			flashDiv++
		}
	}
	total := len(results)
	fmt.Println("aggregate:")
	fmt.Printf("  perfect parity:   %d/%d (%.1f%%)\n", perfect, total, pct(perfect, total))
	fmt.Printf("  any divergence:   %d\n", hasDiverg)
	fmt.Printf("  parse errors:     %d\n", errs)
	fmt.Println("  divergence by category (demos affected, not row counts):")
	fmt.Printf("    per-player K/D/Dmg/Rds:  %d\n", anyPlayerDiv)
	fmt.Printf("    duel_segs:               %d\n", duelDiv)
	fmt.Printf("    kills:                   %d\n", killDiv)
	fmt.Printf("    damages:                 %d\n", dmgDiv)
	fmt.Printf("    weapon_fires:            %d\n", fireDiv)
	fmt.Printf("    weapon_stats:            %d\n", weaponDiv)
	fmt.Printf("    death_events:            %d\n", deathDiv)
	fmt.Printf("    flash_events:            %d\n", flashDiv)
}

func pct(n, total int) float64 {
	if total == 0 {
		return 0
	}
	return 100 * float64(n) / float64(total)
}
