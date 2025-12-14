package cbor

func makePair(key, value []byte) (p Pair) {
	p.Key = key
	p.Value = value
	return
}

// Pair holds an *already CBOR-encoded* key and value.
// Keys are compared bytewise for canonical ordering.
type Pair struct {
	Key   []byte
	Value []byte
}
