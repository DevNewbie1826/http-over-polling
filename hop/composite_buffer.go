package hop

import "io"

type CompositeBuffer struct {
	buf []byte
	off int
}

func (b *CompositeBuffer) Write(p []byte) (int, error) {
	return b.WriteClone(p)
}

func (b *CompositeBuffer) WriteClone(p []byte) (int, error) {
	b.buf = append(b.buf, p...)
	return len(p), nil
}

func (b *CompositeBuffer) WriteAlias(p []byte) (int, error) {
	b.buf = p
	b.off = 0
	return len(p), nil
}

func (b *CompositeBuffer) Read(p []byte) (int, error) {
	if b.off >= len(b.buf) {
		return 0, io.EOF
	}
	n := copy(p, b.buf[b.off:])
	b.off += n
	if b.off >= len(b.buf) {
		return n, io.EOF
	}
	return n, nil
}

func (b *CompositeBuffer) Close() error {
	b.Reset()
	return nil
}

func (b *CompositeBuffer) Len() int {
	if b.off >= len(b.buf) {
		return 0
	}
	return len(b.buf) - b.off
}

func (b *CompositeBuffer) Reset() {
	b.buf = b.buf[:0]
	b.off = 0
}
