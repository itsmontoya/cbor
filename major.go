package cbor

const (
	majorUnsigned major = iota
	majorNegative
	majorBytes
	majorText
	majorArray
	majorMap
	majorTag
	majorSimple
)

// major represents a CBOL major type
type major int

// Byte returns the encoded 3-bit major << 5, ready to OR with the Additional Info.
func (m major) Byte() byte {
	return byte(m) << 5
}

// String is optional but nice for debugging and dumps.
func (m major) String() string {
	switch m {
	case majorUnsigned:
		return "Unsigned"
	case majorNegative:
		return "Negative"
	case majorBytes:
		return "Bytes"
	case majorText:
		return "Text"
	case majorArray:
		return "Array"
	case majorMap:
		return "Map"
	case majorTag:
		return "Tag"
	case majorSimple:
		return "Simple"
	default:
		return "Unknown"
	}
}
