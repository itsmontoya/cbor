package cbor

import (
	"bytes"
	"testing"

	fxcbor "github.com/fxamacker/cbor/v2"
)

func TestEncoder_Encode(t *testing.T) {
	type testcase struct {
		name string // description of this test case

		opt EncoderOpts
		v   testtype

		wantErr bool
	}

	// helper to build a testtype with a pointer
	withPtr := func(base testtype, n int) testtype {
		p := n
		base.Ptr = &p
		return base
	}

	tests := []testcase{
		{
			name: "zero values",
			opt:  EncoderOpts{},
			v:    testtype{},
		},
		{
			name: "simple scalars and collections",
			opt:  EncoderOpts{},
			v: testtype{
				Bool:    true,
				Int:     -1,
				Int8:    -8,
				Int16:   16,
				Int32:   -32,
				Int64:   64,
				Uint:    42,
				Uint8:   8,
				Uint16:  16,
				Uint32:  32,
				Uint64:  64,
				Float32: 1.5,
				Float64: -2.25,

				String: "hello",
				Bytes:  []byte{0x01, 0x02, 0x03},

				Slice: []int{1, 2, 3},
				Array: [2]int{7, 8},
				Map:   map[string]int{"a": 1, "b": 2},

				// OmitEmpty is zero here; just ensures it doesn't blow up.
				OmitEmpty: "",
				Skipped:   123, // should be ignored by encoder
			},
		},
		{
			name: "pointer and omitempty populated",
			opt:  EncoderOpts{},
			v: withPtr(testtype{
				Bool:      true,
				String:    "with pointer",
				OmitEmpty: "non-empty",
			}, 99),
		},
	}

	decMode, err := fxcbor.DecOptions{}.DecMode()
	if err != nil {
		t.Fatalf("failed to build cbor decMode: %v", err)
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			buf := bytes.NewBuffer(nil)
			e := NewEncoder(buf, tt.opt)

			gotErr := e.Encode(tt.v)
			if gotErr != nil {
				if !tt.wantErr {
					t.Fatalf("Encode() error = %v, wantErr = false", gotErr)
				}
				return
			}

			if tt.wantErr {
				t.Fatal("Encode() succeeded unexpectedly")
			}

			// Decode using fxamacker/cbor
			var decoded testtype
			if err := decMode.Unmarshal(buf.Bytes(), &decoded); err != nil {
				t.Fatalf("decoding encoded CBOR failed: %v\nbytes=%x", err, buf.Bytes())
			}

			if !tt.v.IsEqual(decoded) {
				t.Fatalf("round-trip mismatch:\n  encoded bytes: %x\n  got:  %#v\n  want: %#v",
					buf.Bytes(), decoded, tt.v)
			}
		})
	}
}

func mustBytes[T flusher](t *testing.T, buf *bytes.Buffer, enc T) []byte {
	t.Helper()
	if err := enc.Flush(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

type flusher interface {
	Flush() error
}

type testtype struct {
	// Basic scalar types
	Bool    bool
	Int     int
	Int8    int8
	Int16   int16
	Int32   int32
	Int64   int64
	Uint    uint
	Uint8   uint8
	Uint16  uint16
	Uint32  uint32
	Uint64  uint64
	Float32 float32
	Float64 float64

	// Strings & bytes
	String string
	Bytes  []byte

	// Collections
	Slice []int
	Array [2]int
	Map   map[string]int

	// Pointer
	Ptr *int

	// Tag behavior (omitempty)
	OmitEmpty string `cbor:"omit_empty,omitempty"`

	// Skipped field
	Skipped int `cbor:"-"`
}

func (a testtype) IsEqual(b testtype) bool {
	// Compare scalars normally
	if a.Bool != b.Bool ||
		a.Int != b.Int ||
		a.Int8 != b.Int8 ||
		a.Int16 != b.Int16 ||
		a.Int32 != b.Int32 ||
		a.Int64 != b.Int64 ||
		a.Uint != b.Uint ||
		a.Uint8 != b.Uint8 ||
		a.Uint16 != b.Uint16 ||
		a.Uint32 != b.Uint32 ||
		a.Uint64 != b.Uint64 ||
		a.Float32 != b.Float32 ||
		a.Float64 != b.Float64 ||
		a.String != b.String ||
		a.OmitEmpty != b.OmitEmpty {
		return false
	}

	// Compare bytes — treat nil and empty equally
	if !bytes.Equal(a.Bytes, b.Bytes) {
		// bytes.Equal(nil, []byte{}) = false, so handle that:
		if !(len(a.Bytes) == 0 && len(b.Bytes) == 0) {
			return false
		}
	}

	// Compare slices — nil vs empty slice should be equal
	if len(a.Slice) != len(b.Slice) {
		if !(len(a.Slice) == 0 && len(b.Slice) == 0) {
			return false
		}
	} else {
		for i := range a.Slice {
			if a.Slice[i] != b.Slice[i] {
				return false
			}
		}
	}

	// Compare arrays normally (arrays can’t be nil)
	if a.Array != b.Array {
		return false
	}

	// Compare maps — nil vs empty equal
	if len(a.Map) != len(b.Map) {
		if !(len(a.Map) == 0 && len(b.Map) == 0) {
			return false
		}
	} else {
		for k, av := range a.Map {
			if bv, ok := b.Map[k]; !ok || bv != av {
				return false
			}
		}
	}

	// Compare pointers — nil-interchangeability allowed
	if a.Ptr == nil && b.Ptr != nil {
		return false
	}
	if a.Ptr != nil && b.Ptr == nil {
		return false
	}
	if a.Ptr != nil && b.Ptr != nil && *a.Ptr != *b.Ptr {
		return false
	}

	// Skipped field doesn’t participate in encoding or comparison
	return true
}
