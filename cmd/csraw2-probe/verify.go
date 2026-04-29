//go:build ignore

// verify.go — run with: go run cmd/csraw2-probe/verify.go <file.csraw2.tar>
//
// Reads a .csraw2.tar back through csraw2.Read and prints summary stats
// to confirm the on-disk format round-trips correctly.
package main

import (
	"fmt"
	"os"

	"github.com/pable/go-cs-metrics/internal/csraw2"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: verify <file.csraw2.tar>")
		os.Exit(2)
	}
	f, err := os.Open(os.Args[1])
	if err != nil {
		fmt.Fprintf(os.Stderr, "open: %v\n", err)
		os.Exit(1)
	}
	defer f.Close()
	m, err := csraw2.Read(f)
	if err != nil {
		fmt.Fprintf(os.Stderr, "read: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("read OK\n")
	fmt.Printf("  csraw_version: %d\n", m.Header.CSRawVersion)
	fmt.Printf("  map: %s rounds: %d players: %d\n",
		m.Header.Map, len(m.Header.Rounds), len(m.Header.Players))
	fmt.Printf("  kills=%d damages=%d fires=%d flashes=%d\n",
		len(m.Kills), len(m.Damages), len(m.WeaponFires), len(m.Flashes))
	fmt.Printf("  player_samples=%d projectile_samples=%d\n",
		len(m.PlayerSamples), len(m.ProjectileSamples))
	if len(m.Kills) > 0 {
		k := m.Kills[0]
		fmt.Printf("  first kill: tick=%d round=%d killer_slot=%d victim_slot=%d weapon_id=%d hs=%v wallbang=%v\n",
			k.Tick, k.Round, k.KillerSlot, k.VictimSlot, k.WeaponID, k.IsHeadshot, k.IsWallbang)
	}
	if len(m.PlayerSamples) > 0 {
		s := m.PlayerSamples[0]
		fmt.Printf("  first sample: tick=%d slot=%d hp=%d money=%d weapon=%d clip=%d vel=(%d,%d,%d) vis_mask=%016b\n",
			s.Tick, s.PlayerSlot, s.HP, s.Money, s.ActiveWeaponID, s.ClipAmmo,
			s.VelX, s.VelY, s.VelZ, s.VisibleEnemiesMask)
	}
}
