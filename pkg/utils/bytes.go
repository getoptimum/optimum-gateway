package utils

import (
	"encoding/binary"
	"unsafe"
)

func Uint64ToBytes(i uint64) []byte {
	buf := make([]byte, 8)
	binary.LittleEndian.PutUint64(buf, i)
	return buf
}

// UnsafeCastToString casts a byte slice to a string object without performing a copy. Changes
// to byteSlice will also modify the contents of the string, so it is the caller's responsibility
// to ensure that the byte slice will not modified after the string is created.
func UnsafeCastToString(byteSlice []byte) string {
	return *(*string)(unsafe.Pointer(&byteSlice)) // #nosec G103
}
