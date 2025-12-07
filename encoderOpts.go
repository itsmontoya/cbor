package cbor

var DefaultEncOptions = EncoderOpts{
	Canonical:       false,
	MinimizeNumbers: true,
}

type EncoderOpts struct {
	// Canonical per RFC 8949 §4.2 (smallest-length encodings, sorted map keys).
	Canonical bool
	// Deterministically choose smallest width for numbers where exact (e.g., float32 vs float64).
	MinimizeNumbers bool
}
