package parser

import "bytes"

type requestLine struct {
	Method Method
	Target []byte
	Major  uint8
	Minor  uint8
}

type responseLine struct {
	Major      uint8
	Minor      uint8
	StatusCode uint16
	Reason     []byte
}

func parseRequestLine(c *cursor) (requestLine, bool) {
	lineEnd, ok := scanCRLF(c)
	if !ok {
		return requestLine{}, false
	}

	return parseRequestLineBytes(c.slice(lineEnd))
}

func parseRequestLineBytes(line []byte) (requestLine, bool) {
	firstSpace, ok := scanToByte(line, ' ')
	if !ok {
		return requestLine{}, false
	}
	secondSpace, ok := scanToByte(line[firstSpace+1:], ' ')
	if !ok {
		return requestLine{}, false
	}
	secondSpace += firstSpace + 1

	method, ok := matchMethod(line[:firstSpace])
	if !ok {
		return requestLine{}, false
	}
	major, minor, ok := parseHTTPVersionToken(line[secondSpace+1:])
	if !ok {
		return requestLine{}, false
	}

	return requestLine{
		Method: method,
		Target: line[firstSpace+1 : secondSpace],
		Major:  major,
		Minor:  minor,
	}, true
}

func parseResponseLine(c *cursor) (responseLine, bool) {
	lineEnd, ok := scanCRLF(c)
	if !ok {
		return responseLine{}, false
	}

	return parseResponseLineBytes(c.slice(lineEnd))
}

func parseResponseLineBytes(line []byte) (responseLine, bool) {
	if len(line) < len("HTTP/1.1 1 X") || !bytes.Equal(line[:5], []byte("HTTP/")) {
		return responseLine{}, false
	}
	major, minor, ok := parseHTTPVersionDigits(line[5:])
	if !ok {
		return responseLine{}, false
	}
	if len(line) < 12 || line[8] != ' ' {
		return responseLine{}, false
	}

	statusDigits := line[9:12]
	status32, ok := parseDecimalInt32(statusDigits)
	if !ok {
		return responseLine{}, false
	}

	reason := []byte(nil)
	if len(line) > 13 {
		reason = line[13:]
	}

	return responseLine{
		Major:      major,
		Minor:      minor,
		StatusCode: uint16(status32),
		Reason:     reason,
	}, true
}

func scanCRLF(c *cursor) (int, bool) {
	for i := c.pos; i+1 < len(c.buf); i++ {
		if c.buf[i] == '\r' && c.buf[i+1] == '\n' {
			return i - c.pos, true
		}
	}
	return 0, false
}

func parseHTTPVersionToken(b []byte) (uint8, uint8, bool) {
	if len(b) != 8 || !bytes.Equal(b[:5], []byte("HTTP/")) {
		return 0, 0, false
	}
	return parseHTTPVersionDigits(b[5:])
}
