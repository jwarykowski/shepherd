package todo

import (
	"crypto/rand"
	"encoding/hex"
	"time"
)

// NewID mints a stable, opaque item ID: a 32-char lowercase hex string whose
// first 6 bytes are the big-endian unix-millisecond timestamp (so IDs sort by
// creation time) and whose last 10 bytes are crypto-random (80 bits — safe
// against collision even when many agents mint into the same board within one
// millisecond). It's a package var so tests can pin it, mirroring Now/Today.
//
// ponytail: hex over ULID/base32 — stdlib, byte-order-preserving (so the sort
// property holds), no hand-rolled Crockford alphabet. 32 chars is longer than a
// ULID's 26, but agents address items by ID and never type it, so length is
// free. Swap the encoding only if a human ever has to read one aloud.
var NewID = func() string {
	var b [16]byte
	ms := uint64(time.Now().UnixMilli())
	b[0], b[1], b[2] = byte(ms>>40), byte(ms>>32), byte(ms>>24)
	b[3], b[4], b[5] = byte(ms>>16), byte(ms>>8), byte(ms)
	_, _ = rand.Read(b[6:]) // crypto/rand.Read only fails on catastrophic OS error
	return hex.EncodeToString(b[:])
}
