//go:build race

package hop

func bytesToStringForHandler(buf []byte) string {
	if len(buf) == 0 {
		return ""
	}
	return string(buf)
}

func bodyViewForHandler(buf []byte) []byte {
	if len(buf) == 0 {
		return nil
	}
	cloned := make([]byte, len(buf))
	copy(cloned, buf)
	return cloned
}
