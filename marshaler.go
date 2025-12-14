package cbor

type Marshaler interface {
	MarshalCBOR(e *RawEncoder) error
	UnmarshalCBOR(d *RawDecoder) error
}
