package cbor

import (
	"bufio"
	"encoding/binary"
	"errors"
	"io"
	"math"
)

var (
	ErrUnexpectedMajorType   = errors.New("cbor: unexpected major type")
	ErrInvalidAdditionalInfo = errors.New("cbor: invalid additional info")
	ErrIndefiniteLength      = errors.New("cbor: indefinite length not supported for this operation")
	ErrLengthTooLarge        = errors.New("cbor: length too large")
)

// NewRawDecoder wraps r with a buffered reader.
func NewRawDecoder(r io.Reader) *RawDecoder {
	return &RawDecoder{
		r: getBufioReader(r),
	}
}

type RawDecoder struct {
	r       *bufio.Reader
	scratch [8]byte
}

// ---- Core head reader ----

type head struct {
	maj   major
	ai    byte
	value uint64 // for definite lengths / small ints
	indef bool
}

// readHead reads the initial byte and any following length/value bytes.
func (d *RawDecoder) readHead() (h head, err error) {
	b, err := d.r.ReadByte()
	if err != nil {
		return h, err
	}

	h.maj = major(b >> 5)
	h.ai = b & 0x1f

	switch {
	case h.ai < 24:
		h.value = uint64(h.ai)
		return h, nil
	case h.ai == 24:
		if _, err = io.ReadFull(d.r, d.scratch[:1]); err != nil {
			return h, err
		}
		h.value = uint64(d.scratch[0])
		return h, nil
	case h.ai == 25:
		if _, err = io.ReadFull(d.r, d.scratch[:2]); err != nil {
			return h, err
		}
		h.value = uint64(binary.BigEndian.Uint16(d.scratch[:2]))
		return h, nil
	case h.ai == 26:
		if _, err = io.ReadFull(d.r, d.scratch[:4]); err != nil {
			return h, err
		}
		h.value = uint64(binary.BigEndian.Uint32(d.scratch[:4]))
		return h, nil
	case h.ai == 27:
		if _, err = io.ReadFull(d.r, d.scratch[:8]); err != nil {
			return h, err
		}
		h.value = binary.BigEndian.Uint64(d.scratch[:8])
		return h, nil
	case h.ai == aiIndefinite:
		h.indef = true
		return h, nil
	default:
		return h, ErrInvalidAdditionalInfo
	}
}

// readN allocates and reads n bytes.
func (d *RawDecoder) readN(n uint64) ([]byte, error) {
	if n > math.MaxInt {
		return nil, ErrLengthTooLarge
	}
	buf := make([]byte, n)
	if _, err := io.ReadFull(d.r, buf); err != nil {
		return nil, err
	}
	return buf, nil
}

// ---- Numbers ----

// Uint decodes a CBOR unsigned integer (major 0).
func (d *RawDecoder) Uint() (uint64, error) {
	h, err := d.readHead()
	if err != nil {
		return 0, err
	}
	if h.maj != majorUnsigned || h.indef {
		return 0, ErrUnexpectedMajorType
	}
	return h.value, nil
}

// Nint decodes a CBOR negative integer (major 1) as int64: -(1+u).
func (d *RawDecoder) Nint() (int64, error) {
	h, err := d.readHead()
	if err != nil {
		return 0, err
	}
	if h.maj != majorNegative || h.indef {
		return 0, ErrUnexpectedMajorType
	}
	// value encodes u where n = -1 - u
	if h.value > math.MaxInt64 {
		return 0, ErrLengthTooLarge
	}
	n := -1 - int64(h.value)
	return n, nil
}

// Int decodes either unsigned or negative integer as int64.
func (d *RawDecoder) Int() (int64, error) {
	h, err := d.readHead()
	if err != nil {
		return 0, err
	}
	if h.indef {
		return 0, ErrUnexpectedMajorType
	}
	switch h.maj {
	case majorUnsigned:
		if h.value > math.MaxInt64 {
			return 0, ErrLengthTooLarge
		}
		return int64(h.value), nil
	case majorNegative:
		if h.value > math.MaxInt64 {
			return 0, ErrLengthTooLarge
		}
		return -1 - int64(h.value), nil
	default:
		return 0, ErrUnexpectedMajorType
	}
}

func (d *RawDecoder) Float64() (float64, error) {
	h, err := d.readHead()
	if err != nil {
		return 0, err
	}
	if h.maj != majorSimple {
		return 0, ErrUnexpectedMajorType
	}

	switch h.ai {
	case 25: // half-precision (we stored the 16-bit payload in h.value)
		u := uint16(h.value)
		return float64(decodeFloat16(u)), nil

	case 26: // float32 (32-bit payload already in h.value)
		u := uint32(h.value)
		return float64(math.Float32frombits(u)), nil

	case 27: // float64 (64-bit payload already in h.value)
		u := h.value
		return math.Float64frombits(u), nil

	default:
		return 0, ErrUnexpectedMajorType
	}
}

func (d *RawDecoder) Float32() (float32, error) {
	f, err := d.Float64()
	return float32(f), err
}

// decodeFloat16 converts IEEE 754 half-precision to float32.
func decodeFloat16(u uint16) float32 {
	sign := (u >> 15) & 0x1
	exp := (u >> 10) & 0x1f
	frac := u & 0x3ff

	var f float32
	switch exp {
	case 0:
		// subnormal
		f = float32(frac) / (1 << 10) * float32(math.Pow(2, -14))
	case 0x1f:
		if frac == 0 {
			f = float32(math.Inf(1))
		} else {
			f = float32(math.NaN())
		}
	default:
		f = (1 + float32(frac)/(1<<10)) * float32(math.Pow(2, float64(exp-15)))
	}
	if sign == 1 {
		f = -f
	}
	return f
}

// ---- Simple values ----

func (d *RawDecoder) Bool() (bool, error) {
	h, err := d.readHead()
	if err != nil {
		return false, err
	}
	if h.maj != majorSimple || h.indef {
		return false, ErrUnexpectedMajorType
	}
	switch h.ai {
	case 20:
		return false, nil
	case 21:
		return true, nil
	default:
		return false, ErrUnexpectedMajorType
	}
}

func (d *RawDecoder) Null() error {
	h, err := d.readHead()
	if err != nil {
		return err
	}
	if h.maj != majorSimple || h.ai != 22 || h.indef {
		return ErrUnexpectedMajorType
	}
	return nil
}

func (d *RawDecoder) Undefined() error {
	h, err := d.readHead()
	if err != nil {
		return err
	}
	if h.maj != majorSimple || h.ai != 23 || h.indef {
		return ErrUnexpectedMajorType
	}
	return nil
}

// ---- Byte & text strings ----

func (d *RawDecoder) Bytes() ([]byte, error) {
	h, err := d.readHead()
	if err != nil {
		return nil, err
	}
	if h.maj != majorBytes {
		return nil, ErrUnexpectedMajorType
	}
	if h.indef {
		// For now, force caller to use streaming API if they want indefinite.
		return nil, ErrIndefiniteLength
	}
	return d.readN(h.value)
}

// For streaming indefinite byte strings:
//
//   n, err := d.BytesStreamStart()
//   if n >= 0: definite, you probably meant Bytes()
//   else: loop BytesStreamChunk until BytesStreamEnd()

func (d *RawDecoder) BytesStreamStart() (indef bool, length int, err error) {
	h, err := d.readHead()
	if err != nil {
		return false, 0, err
	}
	if h.maj != majorBytes {
		return false, 0, ErrUnexpectedMajorType
	}
	if h.indef {
		return true, -1, nil
	}
	if h.value > math.MaxInt {
		return false, 0, ErrLengthTooLarge
	}
	return false, int(h.value), nil
}

// BytesStreamChunk decodes a single definite-length chunk in an indefinite byte string.
// Caller should stop when BytesStreamEnd returns true.
func (d *RawDecoder) BytesStreamChunk() (chunk []byte, done bool, err error) {
	// Peek next byte: could be break or new bytes head
	b, err := d.r.ReadByte()
	if err != nil {
		return nil, false, err
	}
	if major(b>>5) == majorSimple && (b&0x1f) == 31 {
		// break
		return nil, true, nil
	}
	// Not a break: push back and read head/chunk
	if err := d.r.UnreadByte(); err != nil {
		return nil, false, err
	}
	h, err := d.readHead()
	if err != nil {
		return nil, false, err
	}
	if h.maj != majorBytes || h.indef {
		return nil, false, ErrUnexpectedMajorType
	}
	data, err := d.readN(h.value)
	return data, false, err
}

// BytesStreamEnd is a convenience if you already saw the break via BytesStreamChunk.
// If you're using BytesStreamChunk, you don't strictly need this.
func (d *RawDecoder) BytesStreamEnd() error {
	// Expect a break code (simple, ai=31)
	b, err := d.r.ReadByte()
	if err != nil {
		return err
	}
	if major(b>>5) != majorSimple || (b&0x1f) != 31 {
		return ErrUnexpectedMajorType
	}
	return nil
}

func (d *RawDecoder) String() (string, error) {
	h, err := d.readHead()
	if err != nil {
		return "", err
	}
	if h.maj != majorText {
		return "", ErrUnexpectedMajorType
	}
	if h.indef {
		return "", ErrIndefiniteLength
	}
	data, err := d.readN(h.value)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// ---- Arrays & Maps ----

// ArrayStart returns the array length, or -1 for indefinite-length arrays.
func (d *RawDecoder) ArrayStart() (int, error) {
	h, err := d.readHead()
	if err != nil {
		return 0, err
	}
	if h.maj != majorArray {
		return 0, ErrUnexpectedMajorType
	}
	if h.indef {
		return -1, nil
	}
	if h.value > math.MaxInt {
		return 0, ErrLengthTooLarge
	}
	return int(h.value), nil
}

// ArrayEnd consumes the break code for an indefinite array.
func (d *RawDecoder) ArrayEnd() error {
	b, err := d.r.ReadByte()
	if err != nil {
		return err
	}
	if major(b>>5) != majorSimple || (b&0x1f) != 31 {
		return ErrUnexpectedMajorType
	}
	return nil
}

// MapStart returns the map length (pair count), or -1 for indefinite-length maps.
func (d *RawDecoder) MapStart() (int, error) {
	h, err := d.readHead()
	if err != nil {
		return 0, err
	}
	if h.maj != majorMap {
		return 0, ErrUnexpectedMajorType
	}
	if h.indef {
		return -1, nil
	}
	if h.value > math.MaxInt {
		return 0, ErrLengthTooLarge
	}
	return int(h.value), nil
}

// MapEnd consumes the break code for an indefinite map.
func (d *RawDecoder) MapEnd() error {
	b, err := d.r.ReadByte()
	if err != nil {
		return err
	}
	if major(b>>5) != majorSimple || (b&0x1f) != 31 {
		return ErrUnexpectedMajorType
	}
	return nil
}

// ---- Tags ----

// Tag decodes the next item as a CBOR tag and returns its numeric value.
func (d *RawDecoder) Tag() (uint64, error) {
	h, err := d.readHead()
	if err != nil {
		return 0, err
	}
	if h.maj != majorTag || h.indef {
		return 0, ErrUnexpectedMajorType
	}
	return h.value, nil
}
