package csraw2

import (
	"archive/tar"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/parquet-go/parquet-go"
)

// Read decodes a .csraw2.tar archive from r into a Match. The archive is
// fully buffered in memory; per-match payload is small enough (a few MB)
// that streaming each parquet stream individually buys little.
func Read(r io.Reader) (*Match, error) {
	files, err := readArchive(r)
	if err != nil {
		return nil, err
	}
	return decodeMatch(files)
}

func readArchive(r io.Reader) (map[string][]byte, error) {
	tr := tar.NewReader(r)
	files := map[string][]byte{}
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("tar next: %w", err)
		}
		data, err := io.ReadAll(tr)
		if err != nil {
			return nil, fmt.Errorf("tar read %s: %w", hdr.Name, err)
		}
		files[hdr.Name] = data
	}
	return files, nil
}

func decodeMatch(files map[string][]byte) (*Match, error) {
	headerBytes, ok := files[HeaderName]
	if !ok {
		return nil, fmt.Errorf("missing %s in archive", HeaderName)
	}

	m := &Match{}
	if err := json.Unmarshal(headerBytes, &m.Header); err != nil {
		return nil, fmt.Errorf("unmarshal header: %w", err)
	}
	if m.Header.CSRawVersion != Version {
		return nil, fmt.Errorf("csraw version %d not supported (build expects %d)",
			m.Header.CSRawVersion, Version)
	}

	var err error
	if m.Kills, err = readStream[Kill](files, StreamKills); err != nil {
		return nil, err
	}
	if m.Damages, err = readStream[Damage](files, StreamDamages); err != nil {
		return nil, err
	}
	if m.WeaponFires, err = readStream[WeaponFire](files, StreamWeaponFires); err != nil {
		return nil, err
	}
	if m.Flashes, err = readStream[Flash](files, StreamFlashes); err != nil {
		return nil, err
	}
	if m.FirstSights, err = readStream[FirstSight](files, StreamFirstSights); err != nil {
		return nil, err
	}
	if m.GrenadeThrows, err = readStream[GrenadeThrow](files, StreamGrenadeThrows); err != nil {
		return nil, err
	}
	if m.GrenadeDetonations, err = readStream[GrenadeDetonate](files, StreamGrenadeDetonations); err != nil {
		return nil, err
	}
	if m.BombActions, err = readStream[BombAction](files, StreamBombActions); err != nil {
		return nil, err
	}
	if m.ItemPickups, err = readStream[ItemPickup](files, StreamItemPickups); err != nil {
		return nil, err
	}
	if m.ItemDrops, err = readStream[ItemDrop](files, StreamItemDrops); err != nil {
		return nil, err
	}
	if m.EquipChanges, err = readStream[EquipChange](files, StreamEquipChanges); err != nil {
		return nil, err
	}
	if m.ChatCommands, err = readStream[ChatCommand](files, StreamChatCommands); err != nil {
		return nil, err
	}
	if m.PlayerSamples, err = readStream[PlayerSample](files, StreamPlayerSamples); err != nil {
		return nil, err
	}
	if m.ProjectileSamples, err = readStream[ProjectileSample](files, StreamProjectileSamples); err != nil {
		return nil, err
	}

	return m, nil
}

func readStream[T any](files map[string][]byte, name string) ([]T, error) {
	data, ok := files[name]
	if !ok || len(data) == 0 {
		return nil, nil
	}
	pr := parquet.NewGenericReader[T](bytes.NewReader(data))
	defer pr.Close()

	n := pr.NumRows()
	if n == 0 {
		return nil, nil
	}
	rows := make([]T, n)
	read := 0
	for read < int(n) {
		nr, err := pr.Read(rows[read:])
		read += nr
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return nil, fmt.Errorf("parquet read %s: %w", name, err)
		}
	}
	return rows[:read], nil
}
