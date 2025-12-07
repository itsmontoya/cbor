package cbor

var DefaultEncOptions = EncoderOpts{
	Canonical:       false,
	MinimizeNumbers: true,
	SortMapKeys:     false,
	MaxKeyScratch:   2 << 10, // 2 KiB
}

type EncoderOpts struct {
	// Canonical per RFC 8949 §4.2 (smallest-length encodings, sorted map keys).
	Canonical bool
	// Deterministically choose smallest width for numbers where exact (e.g., float32 vs float64).
	MinimizeNumbers bool
	// Sort map keys by canonical bytewise order of their CBOR-encoded form.
	SortMapKeys bool
	// When sorting keys, pre-encode keys to this max scratch before falling back to heap.
	MaxKeyScratch int
}
