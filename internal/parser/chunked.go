package parser

func parseChunkSizeLine(c *cursor) (int32, bool) {
	lineEnd, ok := scanCRLF(c)
	if !ok {
		return 0, false
	}

	line := c.slice(lineEnd)
	size, ok := parseChunkSizeLineBytes(line)
	if !ok {
		return 0, false
	}
	c.advance(lineEnd + 2)
	return size, true
}

func parseChunkSizeLineBytes(line []byte) (int32, bool) {
	semi, hasSemi := scanToByte(line, ';')
	if hasSemi {
		line = line[:semi]
	}
	if len(line) == 0 {
		return 0, false
	}

	var size int32
	for _, ch := range line {
		digit := unhex[ch]
		if digit < 0 {
			return 0, false
		}
		size = size*16 + int32(digit)
	}
	return size, true
}

func consumeChunkData(c *cursor, size int32) ([]byte, bool) {
	if size < 0 || c.remaining() < int(size)+2 {
		return nil, false
	}

	data := c.slice(int(size))
	c.advance(int(size))
	if c.remaining() < 2 {
		return nil, false
	}
	if b, ok := c.peek(); !ok || b != '\r' {
		return nil, false
	}
	c.advance(1)
	if b, ok := c.peek(); !ok || b != '\n' {
		return nil, false
	}
	c.advance(1)
	return data, true
}
