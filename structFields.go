package cbor

type structField struct {
	index     int
	name      string
	omitempty bool

	enc encoderFn
	dec decoderFn
}
