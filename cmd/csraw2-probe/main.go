// csraw2-probe is a one-shot tool that converts a single .dem to a
// .csraw2.tar via parserv2 + csraw2.Write, then prints per-stream sizes
// and basic shape stats. Used to validate v2 size estimates against real
// data before any CLI integration.
//
// Usage:
//
//	csraw2-probe <demo.dem> [out.csraw2.tar]
//
// If no output path is given, the tar is written to <demo>.csraw2.tar.
package main

import (
	"archive/tar"
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/pable/go-cs-metrics/internal/csraw2"
	"github.com/pable/go-cs-metrics/internal/parserv2"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: csraw2-probe <demo.dem> [out.csraw2.tar]")
		os.Exit(2)
	}
	demoPath := os.Args[1]
	outPath := ""
	if len(os.Args) >= 3 {
		outPath = os.Args[2]
	} else {
		outPath = demoPath + ".csraw2.tar"
	}

	fi, err := os.Stat(demoPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "stat: %v\n", err)
		os.Exit(1)
	}
	demoMB := float64(fi.Size()) / 1024 / 1024
	fmt.Printf("input:  %s (%.1f MB)\n", filepath.Base(demoPath), demoMB)

	t0 := time.Now()
	m, err := parserv2.ParseDemoV2(demoPath, "personal", "competitive")
	if err != nil {
		fmt.Fprintf(os.Stderr, "parse: %v\n", err)
		os.Exit(1)
	}
	parseDur := time.Since(t0)
	fmt.Printf("parsed in %s\n", parseDur.Round(10*time.Millisecond))

	// Print per-stream row counts before writing.
	fmt.Println()
	fmt.Println("rows per stream:")
	rowsRows := []struct {
		name  string
		count int
	}{
		{"kills", len(m.Kills)},
		{"damages", len(m.Damages)},
		{"weapon_fires", len(m.WeaponFires)},
		{"flashes", len(m.Flashes)},
		{"first_sights", len(m.FirstSights)},
		{"grenade_throws", len(m.GrenadeThrows)},
		{"grenade_detonations", len(m.GrenadeDetonations)},
		{"bomb_actions", len(m.BombActions)},
		{"item_pickups", len(m.ItemPickups)},
		{"item_drops", len(m.ItemDrops)},
		{"equip_changes", len(m.EquipChanges)},
		{"chat_commands", len(m.ChatCommands)},
		{"player_samples", len(m.PlayerSamples)},
		{"projectile_samples", len(m.ProjectileSamples)},
	}
	for _, r := range rowsRows {
		fmt.Printf("  %-22s %d\n", r.name, r.count)
	}

	fmt.Println()
	fmt.Printf("header: map=%s tickrate=%.0f rounds=%d players=%d weapons=%d\n",
		m.Header.Map, m.Header.Tickrate, len(m.Header.Rounds),
		len(m.Header.Players), len(m.Header.WeaponTable))
	fmt.Printf("        date=%s match_type=%s tier=%s\n",
		m.Header.MatchDate, m.Header.MatchType, m.Header.Tier)

	// Write the archive into a buffer first so we can print per-stream sizes
	// before persisting to disk.
	t1 := time.Now()
	var buf bytes.Buffer
	if err := csraw2.Write(&buf, m); err != nil {
		fmt.Fprintf(os.Stderr, "write: %v\n", err)
		os.Exit(1)
	}
	writeDur := time.Since(t1)

	totalKB := float64(buf.Len()) / 1024
	fmt.Println()
	fmt.Printf("encoded in %s — total %s (%.1f KB)\n", writeDur.Round(time.Millisecond),
		fmtBytes(buf.Len()), totalKB)

	// Walk the tar to report per-file sizes.
	type entry struct {
		name string
		size int64
	}
	var entries []entry
	tr := tar.NewReader(bytes.NewReader(buf.Bytes()))
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			fmt.Fprintf(os.Stderr, "tar walk: %v\n", err)
			os.Exit(1)
		}
		if _, err := io.Copy(io.Discard, tr); err != nil {
			fmt.Fprintf(os.Stderr, "tar copy: %v\n", err)
			os.Exit(1)
		}
		entries = append(entries, entry{hdr.Name, hdr.Size})
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].size > entries[j].size })

	fmt.Println()
	fmt.Println("per-stream compressed bytes:")
	for _, e := range entries {
		fmt.Printf("  %-32s %s\n", e.name, fmtBytes(int(e.size)))
	}

	// Compression ratio vs source .dem.
	ratio := float64(fi.Size()) / float64(buf.Len())
	fmt.Printf("\ncompression vs .dem: %.0fx (%.1f MB → %s)\n",
		ratio, demoMB, fmtBytes(buf.Len()))

	// Persist.
	if err := os.WriteFile(outPath, buf.Bytes(), 0644); err != nil {
		fmt.Fprintf(os.Stderr, "write file: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("wrote %s\n", outPath)
}

func fmtBytes(n int) string {
	if n < 1024 {
		return fmt.Sprintf("%d B", n)
	}
	if n < 1024*1024 {
		return fmt.Sprintf("%.1f KB", float64(n)/1024)
	}
	return fmt.Sprintf("%.2f MB", float64(n)/1024/1024)
}
