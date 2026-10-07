package spice

import (
	"encoding/binary"
	"encoding/hex"
	"testing"
)

// Regression coverage for the 2026-10 ZTE CAG server change.
//
// The server assigns the main-channel SPICE connection identifier and echoes it
// in MAIN_INIT at payload[5:9] (little-endian). Every subchannel REDQ must send
// that same value in its connectionID field.
//
// The previous implementation searched MAIN_INIT for the byte sequence
// 02 00 00 00 01 and read the four bytes preceding it. The 2026-10 server does
// not emit that sequence, so the lookup fell through to payload[3:7], producing
// a two-byte-shifted identifier. The server then closed all seven subchannels
// with 0x2a close-link frames and the session failed with
// "ZTE session has no authenticated display channel" (see
// session-20261007-191847.016 in the workspace).
//
// The payloads below are verbatim MAIN_INIT payloads from the recorded captures.
func TestZTEMainInitConnectionID(t *testing.T) {
	cases := []struct {
		name    string
		payload string
		want    uint32
	}{
		{
			// Working 2026-07 capture: MAIN_INIT[5:9] = 0b 1f 3f 38.
			// The old marker heuristic happened to succeed here, which is why
			// the defect stayed hidden until the server changed.
			name:    "working baseline 2026-07",
			payload: "00000000000b1f3f3802000000010000000100000000000000b80b0000fb592f7d00e0ff024d274e274f27502700000000000000005127000000000000000000",
			want:    0x383f1f0b,
		},
		{
			// Broken 2026-10 attempt 1: MAIN_INIT[5:9] = bd 1a 52 10.
			// Old code produced 0x1abd0000 and every subchannel was rejected.
			name:    "failure attempt 1 2026-10",
			payload: "0000000000bd1a521001000000010000000100000000000000b80b00001602d9c300e0ff02a727a827a927aa270000000000000000ab27000000000000000000",
			want:    0x10521abd,
		},
		{
			// Broken 2026-10 attempt 2: MAIN_INIT[5:9] = 0f fb f0 2e.
			name:    "failure attempt 2 2026-10",
			payload: "00000000000ffbf02e01000000010000000100000000000000b80b0000d25cd9c300e0ff02a727a827a927aa270000000000000000ab27000000000000000000",
			want:    0x2ef0fb0f,
		},
		{
			// Broken 2026-10 attempt 3: MAIN_INIT[5:9] = 00 13 2b 47.
			name:    "failure attempt 3 2026-10",
			payload: "000000000000132b4701000000010000000100000000000000b80b0000fce0d9c300e0ff02a727a827a927aa270000000000000000ab27000000000000000000",
			want:    0x472b1300,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			payload, err := hex.DecodeString(tc.payload)
			if err != nil {
				t.Fatalf("decode payload: %v", err)
			}
			if got := zteMainInitConnectionID(payload); got != tc.want {
				t.Errorf("zteMainInitConnectionID() = 0x%08x, want 0x%08x", got, tc.want)
			}
			// The identifier must be exactly the little-endian value carried at
			// payload[5:9]; this is what the server echoes back and validates.
			if got, want := zteMainInitConnectionID(payload), binary.LittleEndian.Uint32(payload[5:9]); got != want {
				t.Errorf("identifier disagrees with payload[5:9]: got 0x%08x want 0x%08x", got, want)
			}
		})
	}
}

// The updated server profile must not be resolved through the legacy
// payload[3:7] fallback, which yields a value shifted by two bytes.
func TestZTEMainInitConnectionIDRejectsLegacyFallback(t *testing.T) {
	payload, err := hex.DecodeString("0000000000bd1a521001000000010000000100000000000000b80b00001602d9c300e0ff02a727a827a927aa270000000000000000ab27000000000000000000")
	if err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	legacy := binary.LittleEndian.Uint32(payload[3:7])
	if legacy != 0x1abd0000 {
		t.Fatalf("unexpected legacy fallback value 0x%08x; test fixture changed", legacy)
	}
	if got := zteMainInitConnectionID(payload); got == legacy {
		t.Errorf("zteMainInitConnectionID() still returns the legacy fallback 0x%08x", legacy)
	}
	// The legacy value must still be recoverable from the payload itself, so the
	// fallback branch remains reachable for older server profiles.
	if got := binary.LittleEndian.Uint32(payload[5:9]); got != 0x10521abd {
		t.Errorf("payload[5:9] = 0x%08x, want 0x10521abd", got)
	}
}

// Short payloads must not panic and must keep the historical fallback behaviour.
func TestZTEMainInitConnectionIDShortPayload(t *testing.T) {
	if got := zteMainInitConnectionID(nil); got != 0 {
		t.Errorf("nil payload = 0x%08x, want 0", got)
	}
	if got := zteMainInitConnectionID([]byte{0x00, 0x01, 0x02}); got != 0 {
		t.Errorf("3-byte payload = 0x%08x, want 0", got)
	}
	// 8-byte payload: below the payload[5:9] window, falls through to the
	// legacy marker scan and then the payload[3:7] fallback.
	short := []byte{0x10, 0x11, 0x12, 0x13, 0x14, 0x15, 0x16, 0x17}
	if got, want := zteMainInitConnectionID(short), binary.LittleEndian.Uint32(short[3:7]); got != want {
		t.Errorf("8-byte payload = 0x%08x, want legacy 0x%08x", got, want)
	}
}
