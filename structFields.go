package cbor

import "reflect"

func makeStructFields(t reflect.Type) []structField {
	n := t.NumField()
	out := make([]structField, 0, n)
	for i := 0; i < n; i++ {
		f := t.Field(i)
		// exported only
		if f.PkgPath != "" { // unexported
			continue
		}

		tag := f.Tag.Get("cbor")
		if tag == "-" {
			continue
		}

		sf := makeStructField(i, tag, f)
		out = append(out, sf)
	}

	return out
}

func makeStructField(i int, tag string, sf reflect.StructField) (out structField) {
	name, omitempty := parseTag(tag, sf.Name)
	out.index = i
	out.name = name
	out.omitempty = omitempty
	out.enc = buildFieldEncoder(sf.Type)

	return
}

type structField struct {
	index     int
	name      string
	omitempty bool
	skip      bool

	enc encoderFn
}

func buildFieldEncoder(t reflect.Type) encoderFn {
	// You could pass an *Encoder to reuse options; this keeps it type-only.
	// We need an Encoder to reach opts; choose to close over options at call time:
	return func(e *Encoder, v reflect.Value) (err error) {
		var fn encoderFn
		if fn, err = e.buildEncoder(t); err != nil {
			return
		}

		return fn(e, v)
	}
}
