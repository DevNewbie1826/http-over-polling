//go:build !race

package hop

import "unsafe"

func bytesToStringForHandler(buf []byte) string {
	if len(buf) == 0 {
		return ""
	}
	return unsafe.String(unsafe.SliceData(buf), len(buf))
}

func bodyViewForHandler(buf []byte) []byte {
	return buf
}
