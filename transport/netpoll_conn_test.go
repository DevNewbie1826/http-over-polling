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

func (c *countingNetpollConnectionForTest) Read(_ []byte) (int, error)         { return 0, io.EOF }
func (c *countingNetpollConnectionForTest) Write(p []byte) (int, error)        { return c.writer.buf.Write(p) }
func (c *countingNetpollConnectionForTest) Close() error                       { return nil }
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
