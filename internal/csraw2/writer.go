package csraw2

import (
	"archive/tar"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"time"

	klauszstd "github.com/klauspost/compress/zstd"
	"github.com/parquet-go/parquet-go"
	"github.com/parquet-go/parquet-go/compress/zstd"
)

// Write encodes m to w as a .csraw2.tar archive: header.json followed by
// one parquet file per stream. Empty streams still produce a parquet file
// with the schema, so readers can rely on every stream being present.
//
// The caller is responsible for flushing/closing w if it has any further
// state of its own (e.g. an *os.File).
func Write(w io.Writer, m *Match) error {
	tw := tar.NewWriter(w)

	headerBytes, err := json.MarshalIndent(&m.Header, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal header: %w", err)
	}
	if err := writeFile(tw, HeaderName, headerBytes); err != nil {
		return err
	}

	if err := writeStream(tw, StreamKills, m.Kills); err != nil {
		return err
	}
	if err := writeStream(tw, StreamDamages, m.Damages); err != nil {
		return err
	}
	if err := writeStream(tw, StreamWeaponFires, m.WeaponFires); err != nil {
		return err
	}
	if err := writeStream(tw, StreamFlashes, m.Flashes); err != nil {
		return err
	}
	if err := writeStream(tw, StreamFirstSights, m.FirstSights); err != nil {
		return err
	}
	if err := writeStream(tw, StreamGrenadeThrows, m.GrenadeThrows); err != nil {
		return err
	}
	if err := writeStream(tw, StreamGrenadeDetonations, m.GrenadeDetonations); err != nil {
		return err
	}
	if err := writeStream(tw, StreamBombActions, m.BombActions); err != nil {
		return err
	}
	if err := writeStream(tw, StreamItemPickups, m.ItemPickups); err != nil {
		return err
	}
	if err := writeStream(tw, StreamItemDrops, m.ItemDrops); err != nil {
		return err
	}
	if err := writeStream(tw, StreamEquipChanges, m.EquipChanges); err != nil {
		return err
	}
	if err := writeStream(tw, StreamChatCommands, m.ChatCommands); err != nil {
		return err
	}
	if err := writeStream(tw, StreamPlayerSamples, m.PlayerSamples); err != nil {
		return err
	}
	if err := writeStream(tw, StreamProjectileSamples, m.ProjectileSamples); err != nil {
		return err
	}

	return tw.Close()
}

func writerOptions() []parquet.WriterOption {
	return []parquet.WriterOption{
		parquet.Compression(&zstd.Codec{Level: klauszstd.SpeedBestCompression}),
	}
}

// writeStream serialises rows to parquet+zstd into a buffer, then drops the
// buffer into the tar archive under the given name. A zero-row stream still
// produces a valid parquet file (schema only, no data).
func writeStream[T any](tw *tar.Writer, name string, rows []T) error {
	var buf bytes.Buffer
	pw := parquet.NewGenericWriter[T](&buf, writerOptions()...)
	if len(rows) > 0 {
		if _, err := pw.Write(rows); err != nil {
			_ = pw.Close()
			return fmt.Errorf("parquet write %s: %w", name, err)
		}
	}
	if err := pw.Close(); err != nil {
		return fmt.Errorf("parquet close %s: %w", name, err)
	}
	return writeFile(tw, name, buf.Bytes())
}

func writeFile(tw *tar.Writer, name string, data []byte) error {
	hdr := &tar.Header{
		Name:     name,
		Mode:     0644,
		Size:     int64(len(data)),
		ModTime:  time.Now(),
		Typeflag: tar.TypeReg,
	}
	if err := tw.WriteHeader(hdr); err != nil {
		return fmt.Errorf("tar header %s: %w", name, err)
	}
	if _, err := tw.Write(data); err != nil {
		return fmt.Errorf("tar body %s: %w", name, err)
	}
	return nil
}
