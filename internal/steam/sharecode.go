// Package steam provides utilities for decoding CS2 match share codes and
// interacting with the Steam Web API and Valve replay servers.
package steam

import (
	"fmt"
	"math/big"
	"strings"
)

// Alphabet is the base-57 dictionary used by CS2 share codes.
// It omits visually ambiguous characters: 0, 1, I, O, g, l.
const shareCodeAlphabet = "ABCDEFGHJKLMNOPQRSTUVWXYZabcdefhijkmnopqrstuvwxyz23456789"

var (
	scBase   = big.NewInt(int64(len(shareCodeAlphabet)))
	scLookup = func() map[byte]int64 {
		m := make(map[byte]int64, len(shareCodeAlphabet))
		for i, c := range []byte(shareCodeAlphabet) {
			m[c] = int64(i)
		}
		return m
	}()
)

// ShareCode holds the three values encoded inside a CS2 match share code.
type ShareCode struct {
	MatchID       uint64
	ReservationID uint64
	TVPort        uint16
}

// Decode decodes a CS2 match share code (e.g. "CSGO-XXXXX-XXXXX-XXXXX-XXXXX")
// into its constituent match identifiers.
func Decode(code string) (ShareCode, error) {
	// Strip prefix and dashes.
	code = strings.ReplaceAll(code, "CSGO-", "")
	code = strings.ReplaceAll(code, "-", "")

	if len(code) != 25 {
		return ShareCode{}, fmt.Errorf("share code: expected 25 characters after stripping, got %d", len(code))
	}

	// Reverse the string, then base-57 decode into a big.Int.
	b := []byte(code)
	for i, j := 0, len(b)-1; i < j; i, j = i+1, j-1 {
		b[i], b[j] = b[j], b[i]
	}

	n := new(big.Int)
	for _, c := range b {
		idx, ok := scLookup[c]
		if !ok {
			return ShareCode{}, fmt.Errorf("share code: invalid character %q", c)
		}
		n.Mul(n, scBase)
		n.Add(n, big.NewInt(idx))
	}

	// n encodes 18 bytes in little-endian order. n.Bytes() is big-endian, so
	// we reverse to produce a little-endian byte slice.
	be := n.Bytes()
	le := make([]byte, 18)
	for i, j := 0, len(be)-1; i < 18 && j >= 0; i, j = i+1, j-1 {
		le[i] = be[j]
	}

	return ShareCode{
		MatchID:       leUint64(le[0:8]),
		ReservationID: leUint64(le[8:16]),
		TVPort:        leUint16(le[16:18]),
	}, nil
}

func leUint64(b []byte) uint64 {
	return uint64(b[0]) | uint64(b[1])<<8 | uint64(b[2])<<16 | uint64(b[3])<<24 |
		uint64(b[4])<<32 | uint64(b[5])<<40 | uint64(b[6])<<48 | uint64(b[7])<<56
}

func leUint16(b []byte) uint16 {
	return uint16(b[0]) | uint16(b[1])<<8
}
