package parser

type headerLine struct {
	Name  []byte
	Value []byte
}

func parseHeaderLine(c *cursor) (headerLine, bool) {
	lineEnd, ok := scanCRLF(c)
	if !ok {
		return headerLine{}, false
	}

	return parseHeaderLineBytes(c.slice(lineEnd))
}

func parseHeaderLineBytes(line []byte) (headerLine, bool) {
	sep, ok := scanToByte(line, ':')
	if !ok {
		return headerLine{}, false
	}

	value := line[sep+1:]
	value = value[skipSpaces(value):]

	return headerLine{
		Name:  line[:sep],
		Value: value,
	}, true
}
