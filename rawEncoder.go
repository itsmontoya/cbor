package cbor

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"io"
	"math"
	"sort"
)

// NewRawEncoder wraps w with a buffered writer.
func NewRawEncoder(w io.Writer, opt EncoderOpts) *RawEncoder {
	var e RawEncoder
	e.opt = opt
	e.w = getBufioWriter(w)
	return &e
}

type RawEncoder struct {
	w   *bufio.Writer
	opt EncoderOpts
	// small scratch for lengths and numeric payloads
	scratch [16]byte
}

// Flush exposes underlying buffer flush (call when you need bytes out).
func (e *RawEncoder) Flush() (err error) {
	return e.w.Flush()
}

func (e *RawEncoder) Uint(u uint64) (err error) {
	return e.writeHead(majorUnsigned, u)
}

func (e *RawEncoder) Nint(u uint64) (err error) {
	// Represents -(1+u)
	return e.writeHead(majorNegative, u)
}

func (e *RawEncoder) Int(i int64) (err error) {
	if i >= 0 {
		return e.Uint(uint64(i))
	}

	return e.Nint(uint64(-1 - i))
}

func (e *RawEncoder) Float64(f float64) (err error) {
	// If minimizing, try exact downcast to float32
	if e.opt.MinimizeNumbers {
		if f32 := float32(f); float64(f32) == f {
			return e.Float32(f32)
		}
		// TODO: optional float16 when exactly representable.
	}

	if err = e.w.WriteByte((byte(majorSimple) << 5) | 27); err != nil {
		return err
	}

	binary.BigEndian.PutUint64(e.scratch[:8], math.Float64bits(f))
	return e.write(e.scratch[:8])
}

func (e *RawEncoder) Float32(f float32) (err error) {
	if e.opt.MinimizeNumbers {
		// TODO: optional float16 when exactly representable.
	}

	if err = e.w.WriteByte((byte(majorSimple) << 5) | 26); err != nil {
		return err
	}

	binary.BigEndian.PutUint32(e.scratch[:4], math.Float32bits(f))
	return e.write(e.scratch[:4])

}

func (e *RawEncoder) Bool(b bool) (err error) {
	if b {
		return e.w.WriteByte((byte(majorSimple) << 5) | 21) // true
	}

	return e.w.WriteByte((byte(majorSimple) << 5) | 20) // false
}

func (e *RawEncoder) Null() (err error) {
	return e.w.WriteByte((byte(majorSimple) << 5) | 22)
}

func (e *RawEncoder) Undefined() (err error) {
	return e.w.WriteByte((byte(majorSimple) << 5) | 23)
}

func (e *RawEncoder) Bytes(b []byte) (err error) {
	if err = e.writeHead(majorBytes, uint64(len(b))); err != nil {
		return err
	}

	return e.write(b)
}

func (e *RawEncoder) BytesStreamStart() (err error) {
	return e.writeIndef(majorBytes)
}

func (e *RawEncoder) BytesStreamChunk(chunk []byte) (err error) {
	if err = e.writeHead(majorBytes, uint64(len(chunk))); err != nil {
		return err
	}

	return e.write(chunk)
}

// BytesStreamCopyFrom streams chunks from r as an indefinite byte string.
// Provide a non-nil buf (recommended size 32–64 KiB) for chunking.
func (e *RawEncoder) BytesStreamCopyFrom(r io.Reader, buf []byte) (total int64, err error) {
	for {
		n, er := r.Read(buf)
		if n > 0 {
			if err = e.BytesStreamChunk(buf[:n]); err != nil {
				return total, err
			}

			total += int64(n)
		}

		if er == io.EOF {
			return total, nil
		}

		if er != nil {
			return total, er
		}
	}
}

func (e *RawEncoder) BytesStreamEnd() (err error) {
	return e.writeBreak()
}

func (e *RawEncoder) String(s string) (err error) {
	if err = e.writeHead(majorText, uint64(len(s))); err != nil {
		return err
	}

	return e.write([]byte(s))
}

func (e *RawEncoder) StringStreamStart() (err error) {
	return e.writeIndef(majorText)
}

func (e *RawEncoder) StringStreamChunk(s string) (err error) {
	if err = e.writeHead(majorText, uint64(len(s))); err != nil {
		return err
	}

	return e.write([]byte(s))
}

func (e *RawEncoder) StringStreamEnd() (err error) {
	return e.writeBreak()
}

// ArrayStart writes the array header. n < 0 => indefinite-length array.
func (e *RawEncoder) ArrayStart(n int) (err error) {
	if n < 0 {
		return e.writeIndef(majorArray)
	}

	return e.writeHead(majorArray, uint64(n))
}

// ArrayEnd writes the break code for an indefinite array.
func (e *RawEncoder) ArrayEnd() (err error) {
	return e.writeBreak()
}

// MapStart writes the map header. n < 0 => indefinite-length map.
func (e *RawEncoder) MapStart(n int) (err error) {
	if n < 0 {
		return e.writeIndef(majorMap)
	}

	return e.writeHead(majorMap, uint64(n))
}

// MapEnd writes the break code for an indefinite map.
func (e *RawEncoder) MapEnd() (err error) {
	return e.writeBreak()
}

func (e *RawEncoder) Tag(tag uint64) (err error) {
	return e.writeHead(majorTag, tag)
}

// EncodeSortedMapPairs emits a definite-length CBOR map with entries sorted
// by the canonical bytewise order of their *encoded keys*.
//
// Callers must ensure Pair.Key and Pair.Value are complete, valid CBOR items.
// This avoids re-encoding and enables fully deterministic output.
func (e *RawEncoder) EncodeSortedMapPairs(pairs []Pair) (err error) {
	// Sort in-place by encoded key bytes (canonical order).
	sort.Slice(pairs, func(i, j int) bool {
		return bytes.Compare(pairs[i].Key, pairs[j].Key) < 0
	})

	// Map header (definite length)
	if err = e.writeHead(majorMap, uint64(len(pairs))); err != nil {
		return err
	}

	// Stream keys/values directly
	for _, p := range pairs {
		if err = e.write(p.Key); err != nil {
			return err
		}

		if err = e.write(p.Value); err != nil {
			return err
		}
	}

	return nil
}

// writeHead writes the initial byte (major<<5 | ai) plus any needed length payload.
func (e *RawEncoder) writeHead(maj major, n uint64) (err error) {
	prefix := byte(maj) << 5
	switch {
	case n < 24:
		return e.w.WriteByte(prefix | byte(n))
	case n <= math.MaxUint8:
		if err = e.w.WriteByte(prefix | 24); err != nil {
			return err
		}

		return e.w.WriteByte(byte(n))
	case n <= math.MaxUint16:
		if err = e.w.WriteByte(prefix | 25); err != nil {
			return err
		}

		binary.BigEndian.PutUint16(e.scratch[:2], uint16(n))
		return e.write(e.scratch[:2])
	case n <= math.MaxUint32:
		if err = e.w.WriteByte(prefix | 26); err != nil {
			return err
		}

		binary.BigEndian.PutUint32(e.scratch[:4], uint32(n))
		return e.write(e.scratch[:4])

	default:
		if err = e.w.WriteByte(prefix | 27); err != nil {
			return err
		}
		binary.BigEndian.PutUint64(e.scratch[:8], n)
		return e.write(e.scratch[:8])
	}
}

func (e *RawEncoder) writeIndef(maj major) (err error) {
	return e.w.WriteByte((byte(maj) << 5) | aiIndefinite)
}

func (e *RawEncoder) writeBreak() (err error) {
	return e.w.WriteByte((byte(majorSimple) << 5) | 31)
}

func (e *RawEncoder) write(bs []byte) (err error) {
	_, err = e.w.Write(bs)
	return
}
