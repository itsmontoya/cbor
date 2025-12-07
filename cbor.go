package cbor

import "errors"

var ErrSortNeedsDefinite = errors.New("cbor: sorting map keys requires a definite-length map")

const aiIndefinite = 31 // Additional Info 0x1f => indefinite length
