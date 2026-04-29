// csraw2-compare runs the aggregator twice on the same .dem — once via
// the direct parser → RawMatch path, once via parserv2 → csraw2.Match →
// csraw2bridge → RawMatch — and reports any divergence in the produced
// metrics. Used to validate that v2 + bridge produces identical output
// to the v1 path, so we can swap them without losing fidelity.
//
// Usage:  csraw2-compare <demo.dem>
package main

import (
	"fmt"
	"os"
	"reflect"
	"sort"
	"time"

	"github.com/pable/go-cs-metrics/internal/aggregator"
	"github.com/pable/go-cs-metrics/internal/csraw2bridge"
	"github.com/pable/go-cs-metrics/internal/model"
	"github.com/pable/go-cs-metrics/internal/parser"
	"github.com/pable/go-cs-metrics/internal/parserv2"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: csraw2-compare <demo.dem>")
		os.Exit(2)
	}
	path := os.Args[1]

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
