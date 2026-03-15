package transport

import (
	"bytes"
	"io"
	"net"
	"testing"
	"time"

	"github.com/cloudwego/netpoll"
)

type testNetpollAddr string

func (a testNetpollAddr) Network() string { return "tcp" }
func (a testNetpollAddr) String() string  { return string(a) }

type testWriteLease struct{ data []byte }

func staticWriteLeaseForTest(data string) WriteLease {
	return &testWriteLease{data: []byte(data)}
}

func (l *testWriteLease) Bytes() []byte { return l.data }
func (l *testWriteLease) Retain() WriteLease {
	return &testWriteLease{data: append([]byte(nil), l.data...)}
}
func (l *testWriteLease) Release() {}

type countingNetpollWriterForTest struct {
	inner            netpoll.Writer
	buf              bytes.Buffer
	flushCalls       int
	writeBinaryCalls int
	writeStringCalls int
	writeByteCalls   int
}

func newCountingNetpollWriterForTest() *countingNetpollWriterForTest {
	writer := &countingNetpollWriterForTest{}
	writer.inner = netpoll.NewWriter(&writer.buf)
	return writer
}

func (w *countingNetpollWriterForTest) reset() {
	w.buf.Reset()
	w.flushCalls = 0
	w.writeBinaryCalls = 0
	w.writeStringCalls = 0
	w.writeByteCalls = 0
	w.inner = netpoll.NewWriter(&w.buf)
}

func (w *countingNetpollWriterForTest) Malloc(n int) ([]byte, error) { return w.inner.Malloc(n) }

func (w *countingNetpollWriterForTest) WriteString(s string) (int, error) {
	w.writeStringCalls++
	return w.inner.WriteString(s)
}

func (w *countingNetpollWriterForTest) WriteBinary(b []byte) (int, error) {
	w.writeBinaryCalls++
	return w.inner.WriteBinary(b)
}

func (w *countingNetpollWriterForTest) WriteByte(b byte) error {
	w.writeByteCalls++
	return w.inner.WriteByte(b)
}

func (w *countingNetpollWriterForTest) WriteDirect(p []byte, remainCap int) error {
	return w.inner.WriteDirect(p, remainCap)
}

func (w *countingNetpollWriterForTest) MallocAck(n int) error { return w.inner.MallocAck(n) }

func (w *countingNetpollWriterForTest) Append(other netpoll.Writer) error {
	if wrapped, ok := other.(*countingNetpollWriterForTest); ok {
		return w.inner.Append(wrapped.inner)
	}
	return w.inner.Append(other)
}

func (w *countingNetpollWriterForTest) Flush() error {
	w.flushCalls++
	return w.inner.Flush()
}

func (w *countingNetpollWriterForTest) MallocLen() int { return w.inner.MallocLen() }

type countingNetpollConnectionForTest struct {
	writer *countingNetpollWriterForTest
	reader netpoll.Reader
	local  net.Addr
	remote net.Addr
	closes int
}

type scriptedNetpollReaderForTest struct {
	data         []byte
	releaseCalls int
}

func (r *scriptedNetpollReaderForTest) Next(n int) ([]byte, error) {
	if n < 0 || n > len(r.data) {
		return nil, io.EOF
	}
	out := r.data[:n]
	r.data = r.data[n:]
	return out, nil
}

func (r *scriptedNetpollReaderForTest) Peek(n int) ([]byte, error) {
	if n < 0 || n > len(r.data) {
		return nil, io.EOF
	}
	return r.data[:n], nil
}

func (r *scriptedNetpollReaderForTest) Skip(n int) error {
	if n < 0 || n > len(r.data) {
		return io.EOF
	}
	r.data = r.data[n:]
	return nil
}

func (r *scriptedNetpollReaderForTest) Until(delim byte) ([]byte, error) {
	idx := bytes.IndexByte(r.data, delim)
	if idx < 0 {
		out := append([]byte(nil), r.data...)
		r.data = nil
		return out, io.EOF
	}
	out := r.data[:idx+1]
	r.data = r.data[idx+1:]
	return out, nil
}

func (r *scriptedNetpollReaderForTest) ReadString(n int) (string, error) {
	b, err := r.Next(n)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func (r *scriptedNetpollReaderForTest) ReadBinary(n int) ([]byte, error) {
	b, err := r.Next(n)
	if err != nil {
		return nil, err
	}
	out := make([]byte, len(b))
	copy(out, b)
	return out, nil
}

func (r *scriptedNetpollReaderForTest) ReadByte() (byte, error) {
	b, err := r.Next(1)
	if err != nil {
		return 0, err
	}
	return b[0], nil
}

func (r *scriptedNetpollReaderForTest) Slice(n int) (netpoll.Reader, error) {
	b, err := r.Next(n)
	if err != nil {
		return nil, err
	}
	out := make([]byte, len(b))
	copy(out, b)
	return &scriptedNetpollReaderForTest{data: out}, nil
}

func (r *scriptedNetpollReaderForTest) Release() error {
	r.releaseCalls++
	return nil
}

func (r *scriptedNetpollReaderForTest) Len() int {
	return len(r.data)
}

func newCountingNetpollConnForTest() (*netpollConn, *countingNetpollWriterForTest) {
	writer := newCountingNetpollWriterForTest()
	conn := &countingNetpollConnectionForTest{
		writer: writer,
		reader: netpoll.NewReader(bytes.NewReader(nil)),
		local:  testNetpollAddr("127.0.0.1:8080"),
		remote: testNetpollAddr("127.0.0.1:12345"),
	}
	return newNetpollConn(conn), writer
}

func (c *countingNetpollConnectionForTest) Read(_ []byte) (int, error)  { return 0, io.EOF }
func (c *countingNetpollConnectionForTest) Write(p []byte) (int, error) { return c.writer.buf.Write(p) }
func (c *countingNetpollConnectionForTest) Close() error {
	c.closes++
	return nil
}
func (c *countingNetpollConnectionForTest) LocalAddr() net.Addr                { return c.local }
func (c *countingNetpollConnectionForTest) RemoteAddr() net.Addr               { return c.remote }
func (c *countingNetpollConnectionForTest) SetDeadline(time.Time) error        { return nil }
func (c *countingNetpollConnectionForTest) SetReadDeadline(time.Time) error    { return nil }
func (c *countingNetpollConnectionForTest) SetWriteDeadline(time.Time) error   { return nil }
func (c *countingNetpollConnectionForTest) Reader() netpoll.Reader             { return c.reader }
func (c *countingNetpollConnectionForTest) Writer() netpoll.Writer             { return c.writer }
func (c *countingNetpollConnectionForTest) IsActive() bool                     { return true }
func (c *countingNetpollConnectionForTest) SetReadTimeout(time.Duration) error { return nil }
func (c *countingNetpollConnectionForTest) SetWriteTimeout(time.Duration) error {
	return nil
}
func (c *countingNetpollConnectionForTest) SetIdleTimeout(time.Duration) error { return nil }
func (c *countingNetpollConnectionForTest) SetOnRequest(netpoll.OnRequest) error {
	return nil
}
func (c *countingNetpollConnectionForTest) AddCloseCallback(netpoll.CloseCallback) error {
	return nil
}

func TestNetpollConnWriteLeaseFlushesOnceExperiment(t *testing.T) {
	conn, writer := newCountingNetpollConnForTest()
	n, err := conn.WriteLease(staticWriteLeaseForTest("body"))
	if err != nil {
		t.Fatalf("WriteLease() error = %v", err)
	}
	if n != len("body") {
		t.Fatalf("WriteLease() bytes = %d, want %d", n, len("body"))
	}
	if writer.writeBinaryCalls != 1 {
		t.Fatalf("WriteBinary calls = %d, want 1", writer.writeBinaryCalls)
	}
	if writer.flushCalls != 1 {
		t.Fatalf("Flush calls = %d, want 1", writer.flushCalls)
	}
}

func TestNetpollConnWriteHeaderAndLeaseFlushesOnceExperiment(t *testing.T) {
	conn, writer := newCountingNetpollConnForTest()
	header := []byte("HTTP/1.1 200 OK\r\nContent-Length: 2\r\n\r\n")
	n, err := conn.WriteHeaderAndLease(header, staticWriteLeaseForTest("ok"))
	if err != nil {
		t.Fatalf("WriteHeaderAndLease() error = %v", err)
	}
	if n != len(header)+len("ok") {
		t.Fatalf("WriteHeaderAndLease() bytes = %d, want %d", n, len(header)+len("ok"))
	}
	if writer.writeBinaryCalls != 2 {
		t.Fatalf("WriteBinary calls = %d, want 2 for header+body", writer.writeBinaryCalls)
	}
	if writer.flushCalls != 1 {
		t.Fatalf("Flush calls = %d, want 1", writer.flushCalls)
	}
}

func TestNetpollConnWritevFlushesPerChunkExperiment(t *testing.T) {
	conn, writer := newCountingNetpollConnForTest()
	n, err := conn.Writev([]byte("a"), []byte("bb"), []byte("ccc"))
	if err != nil {
		t.Fatalf("Writev() error = %v", err)
	}
	if n != 6 {
		t.Fatalf("Writev() bytes = %d, want 6", n)
	}
	if writer.writeBinaryCalls != 3 {
		t.Fatalf("WriteBinary calls = %d, want 3", writer.writeBinaryCalls)
	}
	if writer.flushCalls != 1 {
		t.Fatalf("Flush calls = %d, want 1 for batched Writev path", writer.flushCalls)
	}
}

func BenchmarkNetpollConnWriteHeaderAndLeaseSingleFlushExperiment(b *testing.B) {
	conn, writer := newCountingNetpollConnForTest()
	header := []byte("HTTP/1.1 200 OK\r\nContent-Length: 2\r\n\r\n")
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		writer.reset()
		if _, err := conn.WriteHeaderAndLease(header, staticWriteLeaseForTest("ok")); err != nil {
			b.Fatalf("WriteHeaderAndLease() error = %v", err)
		}
	}
}

func BenchmarkNetpollConnWritevThreeChunksExperiment(b *testing.B) {
	conn, writer := newCountingNetpollConnForTest()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		writer.reset()
		if _, err := conn.Writev([]byte("a"), []byte("bb"), []byte("ccc")); err != nil {
			b.Fatalf("Writev() error = %v", err)
		}
	}
}

func TestNetpollConnCoverageBatch(t *testing.T) {
	t.Run("Write empty payload short-circuits", func(t *testing.T) {
		conn, writer := newCountingNetpollConnForTest()
		n, err := conn.Write([]byte{})
		if err != nil {
			t.Fatalf("Write(empty) error = %v", err)
		}
		if n != 0 {
			t.Fatalf("Write(empty) bytes = %d, want 0", n)
		}
		if writer.writeBinaryCalls != 0 {
			t.Fatalf("WriteBinary calls = %d, want 0", writer.writeBinaryCalls)
		}
		if writer.flushCalls != 0 {
			t.Fatalf("Flush calls = %d, want 0", writer.flushCalls)
		}
	})

	t.Run("Peek returns nil when reader empty", func(t *testing.T) {
		conn, _ := newCountingNetpollConnForTest()
		if got := conn.Peek(nil); got != nil {
			t.Fatalf("Peek(empty) = %q, want nil", got)
		}
	})

	t.Run("Peek returns full buffered bytes when reader non-empty", func(t *testing.T) {
		conn, _ := newCountingNetpollConnForTest()
		raw := conn.conn.(*countingNetpollConnectionForTest)
		raw.reader = &scriptedNetpollReaderForTest{data: []byte("peek-data")}
		got := conn.Peek(nil)
		if string(got) != "peek-data" {
			t.Fatalf("Peek(non-empty) = %q, want %q", got, "peek-data")
		}
	})

	t.Run("PauseRead ResumeRead CompleteRequest are no-op", func(t *testing.T) {
		conn, _ := newCountingNetpollConnForTest()
		conn.PauseRead()
		conn.ResumeRead()
		conn.CompleteRequest()
		if conn.closed.Load() {
			t.Fatalf("closed = true, want false")
		}
	})

	t.Run("Close marks state and delegates to underlying connection", func(t *testing.T) {
		conn, _ := newCountingNetpollConnForTest()
		raw := conn.conn.(*countingNetpollConnectionForTest)
		if err := conn.Close(); err != nil {
			t.Fatalf("Close() error = %v", err)
		}
		if !conn.closed.Load() {
			t.Fatalf("closed = false, want true")
		}
		if raw.closes != 1 {
			t.Fatalf("underlying Close calls = %d, want 1", raw.closes)
		}
	})

	t.Run("Context SetContext LocalAddr RemoteAddr accessors", func(t *testing.T) {
		conn, _ := newCountingNetpollConnForTest()
		if got := conn.Context(); got != nil {
			t.Fatalf("Context(initial) = %#v, want nil", got)
		}
		ctxValue := struct{ id int }{id: 7}
		conn.SetContext(ctxValue)
		if got := conn.Context(); got != ctxValue {
			t.Fatalf("Context(after SetContext) = %#v, want %#v", got, ctxValue)
		}
		if got := conn.LocalAddr(); got.String() != "127.0.0.1:8080" {
			t.Fatalf("LocalAddr = %q, want %q", got.String(), "127.0.0.1:8080")
		}
		if got := conn.RemoteAddr(); got.String() != "127.0.0.1:12345" {
			t.Fatalf("RemoteAddr = %q, want %q", got.String(), "127.0.0.1:12345")
		}
	})

	t.Run("netpollReadLease Retain increments only while active", func(t *testing.T) {
		conn, _ := newCountingNetpollConnForTest()
		raw := conn.conn.(*countingNetpollConnectionForTest)
		activeReader := &scriptedNetpollReaderForTest{data: []byte("abc")}
		raw.reader = activeReader

		lease := conn.AcquireRead()
		if string(lease.Bytes()) != "abc" {
			t.Fatalf("AcquireRead().Bytes() = %q, want %q", lease.Bytes(), "abc")
		}
		readLease := lease.(*netpollReadLease)
		if !readLease.active {
			t.Fatalf("active = false, want true")
		}
		if readLease.refCount.Load() != 1 {
			t.Fatalf("refCount(initial) = %d, want 1", readLease.refCount.Load())
		}

		retained := lease.Retain()
		if retained != lease {
			t.Fatalf("Retain() returned different lease instance")
		}
		if readLease.refCount.Load() != 2 {
			t.Fatalf("refCount(after Retain) = %d, want 2", readLease.refCount.Load())
		}

		lease.Release()
		if !readLease.active {
			t.Fatalf("active after first Release = false, want true")
		}
		if readLease.refCount.Load() != 1 {
			t.Fatalf("refCount(after first Release) = %d, want 1", readLease.refCount.Load())
		}

		retained.Release()
		if readLease.active {
			t.Fatalf("active after second Release = true, want false")
		}
		if readLease.refCount.Load() != 0 {
			t.Fatalf("refCount(after second Release) = %d, want 0", readLease.refCount.Load())
		}
		if activeReader.releaseCalls != 1 {
			t.Fatalf("reader.Release calls = %d, want 1", activeReader.releaseCalls)
		}

		raw.reader = netpoll.NewReader(bytes.NewReader(nil))
		emptyLease := conn.AcquireRead()
		emptyReadLease := emptyLease.(*netpollReadLease)
		emptyLease.Retain()
		if emptyReadLease.refCount.Load() != 1 {
			t.Fatalf("inactive refCount(after Retain) = %d, want 1", emptyReadLease.refCount.Load())
		}
	})

	t.Run("netpollWriteLease Bytes Retain Release", func(t *testing.T) {
		lease := &netpollWriteLease{data: []byte("body")}
		if string(lease.Bytes()) != "body" {
			t.Fatalf("Bytes() = %q, want %q", lease.Bytes(), "body")
		}

		retainedLease := lease.Retain()
		retained, ok := retainedLease.(*netpollWriteLease)
		if !ok {
			t.Fatalf("Retain() returned unexpected type %T", retainedLease)
		}
		if retained == lease {
			t.Fatalf("Retain() returned same pointer, want new lease")
		}
		if string(retained.Bytes()) != "body" {
			t.Fatalf("retained.Bytes() = %q, want %q", retained.Bytes(), "body")
		}

		lease.Release()
		retained.Release()
	})
}
