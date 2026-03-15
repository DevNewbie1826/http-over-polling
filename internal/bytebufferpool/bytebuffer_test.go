package bytebufferpool

import (
	"bytes"
	"io"
	"testing"
)

func TestByteBufferWriteStringByteAndReset(t *testing.T) {
	var b ByteBuffer
	if _, err := b.WriteString("hello"); err != nil {
		t.Fatalf("WriteString() error = %v", err)
	}
	if err := b.WriteByte('!'); err != nil {
		t.Fatalf("WriteByte() error = %v", err)
	}
	if got := b.String(); got != "hello!" {
		t.Fatalf("String() = %q, want %q", got, "hello!")
	}
	b.Set([]byte("reset"))
	b.Reset()
	if got := b.Len(); got != 0 {
		t.Fatalf("Len() = %d, want 0", got)
	}
}

func TestByteBufferBytes(t *testing.T) {
	var b ByteBuffer
	b.WriteString("test")

	got := b.Bytes()
	want := []byte("test")

	if !bytes.Equal(got, want) {
		t.Fatalf("Bytes() = %q, want %q", got, want)
	}

	got[0] = 'b'
	if b.String() != "best" {
		t.Fatalf("Bytes() should expose the underlying slice, got %q", b.String())
	}
}

func TestByteBufferWrite(t *testing.T) {
	var b ByteBuffer

	n, err := b.Write([]byte("hello"))
	if err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	if n != 5 {
		t.Fatalf("Write() returned %d, want 5", n)
	}

	n, err = b.Write([]byte(" world"))
	if err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	if n != 6 {
		t.Fatalf("Write() returned %d, want 6", n)
	}

	if b.String() != "hello world" {
		t.Fatalf("String() = %q, want %q", b.String(), "hello world")
	}
}

func TestByteBufferSetString(t *testing.T) {
	var b ByteBuffer
	b.WriteString("initial")

	b.SetString("replaced")

	if b.String() != "replaced" {
		t.Fatalf("SetString() = %q, want %q", b.String(), "replaced")
	}
	if b.Len() != 8 {
		t.Fatalf("Len() = %d, want 8", b.Len())
	}
}

func TestByteBufferReadFrom(t *testing.T) {
	t.Run("with data", func(t *testing.T) {
		var b ByteBuffer
		r := bytes.NewBufferString("hello from reader")

		n, err := b.ReadFrom(r)
		if err != nil {
			t.Fatalf("ReadFrom() error = %v", err)
		}
		if n != 17 {
			t.Fatalf("ReadFrom() returned %d, want 17", n)
		}
		if b.String() != "hello from reader" {
			t.Fatalf("String() = %q, want %q", b.String(), "hello from reader")
		}
	})

	t.Run("with EOF", func(t *testing.T) {
		var b ByteBuffer
		r := bytes.NewBufferString("")

		n, err := b.ReadFrom(r)
		if err != nil {
			t.Fatalf("ReadFrom() error = %v", err)
		}
		if n != 0 {
			t.Fatalf("ReadFrom() returned %d, want 0", n)
		}
	})

	t.Run("with pre-allocated buffer", func(t *testing.T) {
		var b ByteBuffer
		b.B = make([]byte, 0, 64) // pre-allocate
		r := bytes.NewBufferString("data")

		n, err := b.ReadFrom(r)
		if err != nil {
			t.Fatalf("ReadFrom() error = %v", err)
		}
		if n != 4 {
			t.Fatalf("ReadFrom() returned %d, want 4", n)
		}
		if b.String() != "data" {
			t.Fatalf("String() = %q, want %q", b.String(), "data")
		}
	})

	t.Run("with error reader", func(t *testing.T) {
		var b ByteBuffer
		r := &errorReader{}

		n, err := b.ReadFrom(r)
		if err != io.ErrUnexpectedEOF {
			t.Fatalf("ReadFrom() error = %v, want %v", err, io.ErrUnexpectedEOF)
		}
		if n != 4 {
			t.Fatalf("ReadFrom() returned %d, want 4", n)
		}
		if b.String() != "part" {
			t.Fatalf("String() = %q, want %q", b.String(), "part")
		}
	})

	t.Run("appends to existing buffer and grows", func(t *testing.T) {
		b := ByteBuffer{B: append(make([]byte, 0, 2), 'a', 'b')}
		r := bytes.NewBufferString("cdef")

		n, err := b.ReadFrom(r)
		if err != nil {
			t.Fatalf("ReadFrom() error = %v", err)
		}
		if n != 4 {
			t.Fatalf("ReadFrom() returned %d, want 4", n)
		}
		if b.String() != "abcdef" {
			t.Fatalf("String() = %q, want %q", b.String(), "abcdef")
		}
		if cap(b.B) < len("abcdef") {
			t.Fatalf("cap(B) = %d, want at least %d", cap(b.B), len("abcdef"))
		}
	})
}

func TestByteBufferWriteTo(t *testing.T) {
	t.Run("with data", func(t *testing.T) {
		var b ByteBuffer
		b.WriteString("hello world")

		var buf bytes.Buffer
		n, err := b.WriteTo(&buf)
		if err != nil {
			t.Fatalf("WriteTo() error = %v", err)
		}
		if n != 11 {
			t.Fatalf("WriteTo() returned %d, want 11", n)
		}
		if buf.String() != "hello world" {
			t.Fatalf("WriteTo() wrote %q, want %q", buf.String(), "hello world")
		}
	})

	t.Run("empty buffer", func(t *testing.T) {
		var b ByteBuffer

		var buf bytes.Buffer
		n, err := b.WriteTo(&buf)
		if err != nil {
			t.Fatalf("WriteTo() error = %v", err)
		}
		if n != 0 {
			t.Fatalf("WriteTo() returned %d, want 0", n)
		}
		if buf.Len() != 0 {
			t.Fatalf("WriteTo() should write nothing for empty buffer")
		}
	})

	t.Run("with error writer", func(t *testing.T) {
		var b ByteBuffer
		b.WriteString("test")

		w := &errorWriter{}
		n, err := b.WriteTo(w)
		if err != io.ErrShortWrite {
			t.Fatalf("WriteTo() error = %v, want %v", err, io.ErrShortWrite)
		}
		if n != 3 {
			t.Fatalf("WriteTo() returned %d, want 3", n)
		}
	})
}

func TestByteBufferLen(t *testing.T) {
	t.Run("empty", func(t *testing.T) {
		var b ByteBuffer
		if b.Len() != 0 {
			t.Fatalf("Len() = %d, want 0", b.Len())
		}
	})

	t.Run("with data", func(t *testing.T) {
		var b ByteBuffer
		b.WriteString("hello")
		if b.Len() != 5 {
			t.Fatalf("Len() = %d, want 5", b.Len())
		}
	})

	t.Run("after reset", func(t *testing.T) {
		var b ByteBuffer
		b.WriteString("hello")
		b.Reset()
		if b.Len() != 0 {
			t.Fatalf("Len() = %d, want 0 after Reset", b.Len())
		}
	})
}

func TestByteBufferSet(t *testing.T) {
	var b ByteBuffer
	b.WriteString("initial data")

	b.Set([]byte("new data"))

	if b.String() != "new data" {
		t.Fatalf("Set() = %q, want %q", b.String(), "new data")
	}
	if b.Len() != 8 {
		t.Fatalf("Len() = %d, want 8", b.Len())
	}

	b.Set([]byte("short"))
	if b.Len() != 5 {
		t.Fatalf("Len() = %d, want 5", b.Len())
	}
	if b.String() != "short" {
		t.Fatalf("String() = %q, want %q", b.String(), "short")
	}
}

type errorReader struct{}

func (r *errorReader) Read(p []byte) (int, error) {
	copy(p, "partial")
	return 4, io.ErrUnexpectedEOF
}

type errorWriter struct {
	written int
}

func (w *errorWriter) Write(p []byte) (int, error) {
	w.written = len(p)
	if w.written > 0 {
		return w.written - 1, io.ErrShortWrite
	}
	return 0, io.ErrShortWrite
}
