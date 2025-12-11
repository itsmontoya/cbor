package cbor

import (
	"errors"
	"fmt"
	"io"
	"math"
	"reflect"
)

// NewDecoder wraps r with a buffered raw decoder.
func NewDecoder(r io.Reader) *Decoder {
	var d Decoder
	d.r = NewRawDecoder(r)
	return &d
}

type Decoder struct {
	r *RawDecoder
}

func (d *Decoder) Decode(v any) error {
	if v == nil {
		return errors.New("cbor: Decode(nil)")
	}

	// Custom Marshaler takes precedence.
	if um, ok := v.(Marshaler); ok {
		return um.UnmarshalCBOR(d.r)
	}

	rv := reflect.ValueOf(v)
	if rv.Kind() != reflect.Pointer || rv.IsNil() {
		return errors.New("cbor: Decode(non-pointer)")
	}

	elem := rv.Elem()

	fn, err := d.buildDecoder(elem.Type())
	if err != nil {
		return err
	}

	return fn(d, elem)
}

func (d *Decoder) buildDecoder(t reflect.Type) (decoderFn, error) {
	if fn, ok := dtc.Get(t); ok {
		return fn, nil
	}

	var fn decoderFn
	var err error

	switch t.Kind() {
	case reflect.Bool:
		fn = decodeBool
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		fn, err = d.generateIntDecoder(t)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		fn, err = d.generateUintDecoder(t)
	case reflect.Float32:
		fn = decodeFloat32
	case reflect.Float64:
		fn = decodeFloat64
	case reflect.String:
		fn = decodeString

	case reflect.Slice:
		fn, err = d.generateSliceDecoder(t)
	case reflect.Array:
		fn, err = d.generateArrayDecoder(t)
	case reflect.Map:
		fn, err = d.generateMapDecoder(t)
	case reflect.Struct:
		fn, err = d.generateStructDecoder(t)

	case reflect.Interface:
		fn, err = d.generateInterfaceDecoder()
	case reflect.Pointer:
		fn, err = d.generatePointerDecoder(t)

	default:
		return nil, makeErrorUnsupportedType(t)
	}

	if err != nil {
		return nil, err
	}

	dtc.Set(t, fn)
	return fn, nil
}

func (d *Decoder) makeStructFields(t reflect.Type) ([]structField, error) {
	n := t.NumField()
	out := make([]structField, 0, n)

	for i := 0; i < n; i++ {
		f := t.Field(i)
		if f.PkgPath != "" {
			// unexported
			continue
		}

		tag := f.Tag.Get("cbor")
		if tag == "-" {
			continue
		}

		name, _ := parseTag(tag, f.Name) // omitempty only affects encoding

		dfn, err := d.buildDecoder(f.Type)
		if err != nil {
			return nil, err
		}

		out = append(out, structField{
			index: i,
			name:  name,
			dec:   dfn,
		})
	}

	return out, nil
}

func (d *Decoder) generateIntDecoder(t reflect.Type) (decoderFn, error) {
	bits := t.Bits()
	return func(d *Decoder, v reflect.Value) error {
		n, err := d.r.Int()
		if err != nil {
			return err
		}
		// Range check
		min := -(int64(1) << (bits - 1))
		max := (int64(1) << (bits - 1)) - 1
		if n < min || n > max {
			return errors.New("cbor: int out of range")
		}
		v.SetInt(n)
		return nil
	}, nil
}

func (d *Decoder) generateUintDecoder(t reflect.Type) (decoderFn, error) {
	bits := t.Bits()
	return func(d *Decoder, v reflect.Value) error {
		n, err := d.r.Int()
		if err != nil {
			return err
		}

		if n < 0 {
			return errors.New("cbor: negative value for unsigned")
		}

		u := uint64(n)
		if u > (uint64(1)<<bits)-1 {
			return errors.New("cbor: uint out of range")
		}
		v.SetUint(u)
		return nil
	}, nil
}

func (d *Decoder) generateSliceDecoder(t reflect.Type) (decoderFn, error) {
	// []byte -> byte string
	if t.Elem().Kind() == reflect.Uint8 {
		return func(d *Decoder, v reflect.Value) error {
			bs, err := d.r.Bytes()
			if err != nil {
				return err
			}
			v.SetBytes(bs)
			return nil
		}, nil
	}

	// general slice -> CBOR array
	elemDec, err := d.buildDecoder(t.Elem())
	if err != nil {
		return nil, err
	}

	return func(d *Decoder, v reflect.Value) error {
		n, err := d.r.ArrayStart()
		if err != nil {
			return err
		}
		if n < 0 {
			return ErrIndefiniteLength // you can support this later if you want
		}

		s := reflect.MakeSlice(t, n, n)
		for i := 0; i < n; i++ {
			if err := elemDec(d, s.Index(i)); err != nil {
				return err
			}
		}
		v.Set(s)
		return nil
	}, nil
}

func (d *Decoder) generateArrayDecoder(t reflect.Type) (decoderFn, error) {
	elemDec, err := d.buildDecoder(t.Elem())
	if err != nil {
		return nil, err
	}
	length := t.Len()

	return func(d *Decoder, v reflect.Value) error {
		n, err := d.r.ArrayStart()
		if err != nil {
			return err
		}
		if n != length {
			return errors.New("cbor: array length mismatch")
		}
		for i := 0; i < length; i++ {
			if err := elemDec(d, v.Index(i)); err != nil {
				return err
			}
		}
		return nil
	}, nil
}

func (d *Decoder) generateMapDecoder(t reflect.Type) (decoderFn, error) {
	keyDec, err := d.buildDecoder(t.Key())
	if err != nil {
		return nil, err
	}
	valDec, err := d.buildDecoder(t.Elem())
	if err != nil {
		return nil, err
	}

	return func(d *Decoder, v reflect.Value) error {
		n, err := d.r.MapStart()
		if err != nil {
			return err
		}
		if n < 0 {
			return ErrIndefiniteLength // our encoder doesn't emit indefinite maps for map[K]V
		}

		m := reflect.MakeMapWithSize(t, n)
		for i := 0; i < n; i++ {
			kv := reflect.New(t.Key()).Elem()
			if err := keyDec(d, kv); err != nil {
				return err
			}
			vv := reflect.New(t.Elem()).Elem()
			if err := valDec(d, vv); err != nil {
				return err
			}
			m.SetMapIndex(kv, vv)
		}
		v.Set(m)
		return nil
	}, nil
}

func (d *Decoder) generateStructDecoder(t reflect.Type) (decoderFn, error) {
	fields, err := d.makeStructFields(t)
	if err != nil {
		return nil, err
	}

	// name -> field
	fieldByName := make(map[string]structField, len(fields))
	for _, f := range fields {
		fieldByName[f.name] = f
	}

	return func(d *Decoder, v reflect.Value) error {
		n, err := d.r.MapStart()
		if err != nil {
			return err
		}

		fmt.Println("N", n)

		// ----- Definite-length map -----
		if n >= 0 {
			for i := 0; i < n; i++ {
				key, err := d.r.String()
				if err != nil {
					return err
				}
				sf, ok := fieldByName[key]
				if !ok {
					// TEMP behavior: hard error for unknown fields
					return errors.New("cbor: unknown struct field: " + key)
				}
				fv := v.Field(sf.index)
				if err := sf.dec(d, fv); err != nil {
					return err
				}
			}
			return nil
		}

		// ----- Indefinite-length map -----
		for {
			// Look at the next item head
			h, err := d.r.readHead()
			if err != nil {
				return err
			}

			// Break?
			if h.maj == majorSimple && h.ai == 31 {
				// This is the map break code (0xff)
				return nil
			}

			// Otherwise this must be a text key header.
			if h.maj != majorText || h.indef {
				return ErrUnexpectedMajorType
			}

			if h.value > math.MaxInt {
				return ErrLengthTooLarge
			}

			// Read key bytes based on the length in the head we just read.
			keyBytes, err := d.r.readN(h.value)
			if err != nil {
				return err
			}
			key := string(keyBytes)
			fmt.Println("Key", key)
			sf, ok := fieldByName[key]
			if !ok {
				// TEMP behavior: hard error on unknown fields
				return errors.New("cbor: unknown struct field: " + key)
			}

			fv := v.Field(sf.index)
			if err := sf.dec(d, fv); err != nil {
				return err
			}
		}
	}, nil
}

func (d *Decoder) generatePointerDecoder(t reflect.Type) (decoderFn, error) {
	elem := t.Elem()

	elemDec, err := d.buildDecoder(elem)
	if err != nil {
		return nil, err
	}

	return func(d *Decoder, v reflect.Value) error {
		// Peek the next byte to see if it's CBOR null.
		b, err := d.r.r.ReadByte()
		if err != nil {
			return err
		}
		maj := major(b >> 5)
		ai := b & 0x1f

		// Null: represent as a nil pointer.
		if maj == majorSimple && ai == 22 { // simple(null)
			v.Set(reflect.Zero(v.Type())) // nil *T
			return nil
		}

		// Not null: put the byte back and decode as the element type.
		if err := d.r.r.UnreadByte(); err != nil {
			return err
		}

		ptr := reflect.New(elem)
		if err := elemDec(d, ptr.Elem()); err != nil {
			return err
		}
		v.Set(ptr)
		return nil
	}, nil
}

func (d *Decoder) generateInterfaceDecoder() (decoderFn, error) {
	// For now, we don't have a generic "any" decoder.
	// Easiest: require interface{} as the top-level type and decode into concrete types yourself
	// via Unmarshaler. You can extend this later.
	return func(d *Decoder, v reflect.Value) error {
		return errors.New("cbor: decoding into interface is not yet implemented")
	}, nil
}

func decodeBool(d *Decoder, v reflect.Value) error {
	b, err := d.r.Bool()
	if err != nil {
		return err
	}
	v.SetBool(b)
	return nil
}

func decodeFloat32(d *Decoder, v reflect.Value) error {
	f, err := d.r.Float32()
	if err != nil {
		return err
	}
	v.SetFloat(float64(f))
	return nil
}

func decodeFloat64(d *Decoder, v reflect.Value) error {
	f, err := d.r.Float64()
	if err != nil {
		return err
	}
	v.SetFloat(f)
	return nil
}

func decodeString(d *Decoder, v reflect.Value) error {
	s, err := d.r.String()
	if err != nil {
		return err
	}
	v.SetString(s)
	return nil
}

// decoderFn decodes into v (which is a reflect.Value of the destination).
type decoderFn func(d *Decoder, v reflect.Value) error
