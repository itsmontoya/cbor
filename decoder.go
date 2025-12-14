package cbor

import (
	"errors"
	"io"
	"math"
	"reflect"
)

// NewDecoder wraps r with a buffered raw decoder.
func NewDecoder(r io.Reader) (dec *Decoder) {
	var d Decoder
	d.r = NewRawDecoder(r)
	return &d
}

type Decoder struct {
	r *RawDecoder
}

func (d *Decoder) Decode(v any) (err error) {
	if v == nil {
		return errors.New("cbor: Decode(nil)")
	}

	// Custom Unmarshaler takes precedence.
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

func (d *Decoder) buildDecoder(t reflect.Type) (fn decoderFn, err error) {
	var ok bool
	if fn, ok = dtc.Get(t); ok {
		return
	}

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
	return
}

func (d *Decoder) makeStructFields(t reflect.Type) (out []structField, err error) {
	n := t.NumField()
	out = make([]structField, 0, n)

	for i := range n {
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

		var dfn decoderFn
		if dfn, err = d.buildDecoder(f.Type); err != nil {
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

func (d *Decoder) generateIntDecoder(t reflect.Type) (fn decoderFn, err error) {
	bits := t.Bits()

	fn = func(dec *Decoder, v reflect.Value) (err error) {
		var n int64
		if n, err = dec.r.Int(); err != nil {
			return
		}

		// Range check
		min := -(int64(1) << (bits - 1))
		max := (int64(1) << (bits - 1)) - 1
		if n < min || n > max {
			err = errors.New("cbor: int out of range")
			return
		}

		v.SetInt(n)
		return
	}

	return
}

func (d *Decoder) generateUintDecoder(t reflect.Type) (fn decoderFn, err error) {
	bits := t.Bits()

	fn = func(dec *Decoder, v reflect.Value) (err error) {
		var n int64
		if n, err = dec.r.Int(); err != nil {
			return
		}

		if n < 0 {
			err = errors.New("cbor: negative value for unsigned")
			return
		}

		u := uint64(n)
		if u > (uint64(1)<<bits)-1 {
			err = errors.New("cbor: uint out of range")
			return
		}

		v.SetUint(u)
		return
	}

	return
}

func (d *Decoder) generateSliceDecoder(t reflect.Type) (fn decoderFn, err error) {
	// []byte -> byte string
	if t.Elem().Kind() == reflect.Uint8 {
		fn = func(dec *Decoder, v reflect.Value) (err error) {
			var bs []byte
			bs, err = dec.r.Bytes()
			if err != nil {
				return
			}
			v.SetBytes(bs)
			return
		}
		return
	}

	// general slice -> CBOR array
	var elemDec decoderFn
	if elemDec, err = d.buildDecoder(t.Elem()); err != nil {
		return
	}

	fn = func(dec *Decoder, v reflect.Value) (err error) {
		var n int
		if n, err = dec.r.ArrayStart(); err != nil {
			return
		}

		if n < 0 {
			err = ErrIndefiniteLength // you can support this later if you want
			return
		}

		s := reflect.MakeSlice(t, n, n)
		for i := 0; i < n; i++ {
			if err = elemDec(dec, s.Index(i)); err != nil {
				return
			}
		}

		v.Set(s)
		return
	}

	return
}

func (d *Decoder) generateArrayDecoder(t reflect.Type) (fn decoderFn, err error) {
	var elemDec decoderFn
	if elemDec, err = d.buildDecoder(t.Elem()); err != nil {
		return
	}

	length := t.Len()
	fn = func(dec *Decoder, v reflect.Value) (err error) {
		var n int
		if n, err = dec.r.ArrayStart(); err != nil {
			return err
		}

		if n != length {
			return errors.New("cbor: array length mismatch")
		}

		for i := range length {
			err = elemDec(dec, v.Index(i))
			if err != nil {
				return
			}
		}

		return nil
	}

	return
}

func (d *Decoder) generateMapDecoder(t reflect.Type) (fn decoderFn, err error) {
	var keyDec decoderFn
	if keyDec, err = d.buildDecoder(t.Key()); err != nil {
		return nil, err
	}

	var valDec decoderFn
	if valDec, err = d.buildDecoder(t.Elem()); err != nil {
		return nil, err
	}

	fn = func(dec *Decoder, v reflect.Value) (err error) {
		var n int
		if n, err = dec.r.MapStart(); err != nil {
			return err
		}

		if n < 0 {
			err = ErrIndefiniteLength // our encoder doesn't emit indefinite maps for map[K]V
			return
		}

		m := reflect.MakeMapWithSize(t, n)
		for i := 0; i < n; i++ {
			kv := reflect.New(t.Key()).Elem()
			if err = keyDec(dec, kv); err != nil {
				return err
			}

			vv := reflect.New(t.Elem()).Elem()
			if err = valDec(dec, vv); err != nil {
				return err
			}

			m.SetMapIndex(kv, vv)
		}

		v.Set(m)
		return
	}

	return
}

func (d *Decoder) generateStructDecoder(t reflect.Type) (fn decoderFn, err error) {
	var fields []structField
	fields, err = d.makeStructFields(t)
	if err != nil {
		return
	}

	// name -> field
	fieldByName := make(map[string]structField, len(fields))
	for _, f := range fields {
		fieldByName[f.name] = f
	}

	fn = func(dec *Decoder, v reflect.Value) (err error) {
		var n int
		if n, err = dec.r.MapStart(); err != nil {
			return
		}

		// ----- Definite-length map -----
		if n >= 0 {
			for i := 0; i < n; i++ {
				var key string
				if key, err = dec.r.String(); err != nil {
					return
				}

				var (
					sf structField
					ok bool
				)

				if sf, ok = fieldByName[key]; !ok {
					// TEMP behavior: hard error for unknown fields
					err = errors.New("cbor: unknown struct field: " + key)
					return
				}

				fv := v.Field(sf.index)
				if err = sf.dec(dec, fv); err != nil {
					return
				}
			}

			return
		}

		// ----- Indefinite-length map -----
		for {
			var h head
			if h, err = dec.r.readHead(); err != nil {
				return
			}

			// Break?
			if h.maj == majorSimple && h.ai == 31 {
				// This is the map break code (0xff)
				return
			}

			// Otherwise this must be a text key header.
			if h.maj != majorText || h.indef {
				err = ErrUnexpectedMajorType
				return
			}

			if h.value > math.MaxInt {
				err = ErrLengthTooLarge
				return
			}

			var keyBytes []byte
			if keyBytes, err = dec.r.readN(h.value); err != nil {
				return
			}

			var (
				sf structField
				ok bool
			)

			key := string(keyBytes)
			if sf, ok = fieldByName[key]; !ok {
				// TEMP behavior: hard error on unknown fields
				err = errors.New("cbor: unknown struct field: " + key)
				return
			}

			fv := v.Field(sf.index)
			err = sf.dec(dec, fv)
			if err != nil {
				return
			}
		}
	}

	return
}

func (d *Decoder) generatePointerDecoder(t reflect.Type) (fn decoderFn, err error) {
	var elemDec decoderFn
	elem := t.Elem()
	if elemDec, err = d.buildDecoder(elem); err != nil {
		return
	}

	fn = func(dec *Decoder, v reflect.Value) (err error) {
		// Peek the next byte to see if it's CBOR null.
		var b byte
		if b, err = dec.r.r.ReadByte(); err != nil {
			return
		}

		maj := major(b >> 5)
		ai := b & 0x1f

		// Null: represent as a nil pointer.
		if maj == majorSimple && ai == 22 { // simple(null)
			v.Set(reflect.Zero(v.Type())) // nil *T
			return
		}

		// Not null: put the byte back and decode as the element type.
		if err = dec.r.r.UnreadByte(); err != nil {
			return
		}

		ptr := reflect.New(elem)
		if err = elemDec(dec, ptr.Elem()); err != nil {
			return
		}

		v.Set(ptr)
		return
	}

	return
}

func (d *Decoder) generateInterfaceDecoder() (fn decoderFn, err error) {
	// For now, we don't have a generic "any" decoder.
	// Easiest: require interface{} as the top-level type and decode into concrete types yourself
	// via Unmarshaler. You can extend this later.
	fn = func(_ *Decoder, _ reflect.Value) (err error) {
		err = errors.New("cbor: decoding into interface is not yet implemented")
		return
	}

	return
}

func decodeBool(d *Decoder, v reflect.Value) (err error) {
	var b bool
	if b, err = d.r.Bool(); err != nil {
		return
	}

	v.SetBool(b)
	return
}

func decodeFloat32(d *Decoder, v reflect.Value) (err error) {
	var f float32
	if f, err = d.r.Float32(); err != nil {
		return
	}

	v.SetFloat(float64(f))
	return
}

func decodeFloat64(d *Decoder, v reflect.Value) (err error) {
	var f float64
	if f, err = d.r.Float64(); err != nil {
		return
	}

	v.SetFloat(f)
	return
}

func decodeString(d *Decoder, v reflect.Value) (err error) {
	var s string
	if s, err = d.r.String(); err != nil {
		return
	}

	v.SetString(s)
	return
}

// decoderFn decodes into v (which is a reflect.Value of the destination).
type decoderFn func(d *Decoder, v reflect.Value) error
