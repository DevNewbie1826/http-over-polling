package parser

type cursor struct {
	buf []byte
	pos int
}

func newCursor(buf []byte) cursor {
	return cursor{buf: buf}
}

func (c *cursor) remaining() int {
	if c.pos >= len(c.buf) {
		return 0
	}
	return len(c.buf) - c.pos
}

func (c *cursor) peek() (byte, bool) {
	if c.pos >= len(c.buf) {
		return 0, false
	}
	return c.buf[c.pos], true
}

func (c *cursor) advance(n int) {
	c.pos += n
	if c.pos > len(c.buf) {
		c.pos = len(c.buf)
	}
}

func (c *cursor) slice(n int) []byte {
	end := c.pos + n
	if end > len(c.buf) {
		end = len(c.buf)
	}
	return c.buf[c.pos:end]
}
