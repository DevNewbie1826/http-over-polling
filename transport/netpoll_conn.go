package transport

import (
	"bufio"
	"net"
	"sync"
	"sync/atomic"

	"github.com/cloudwego/netpoll"
)

type netpollConn struct {
	conn        netpoll.Connection
	local       net.Addr
	remote      net.Addr
	ctx         any
	done        chan struct{}
	lease       netpollReadLease
	write       netpollWriteLease
	readHandler func(net.Conn, *bufio.ReadWriter) error
	rw          *bufio.ReadWriter
	hijacked    atomic.Bool
	closed      atomic.Bool
	closeOnce   atomic.Bool
	readPaused  atomic.Bool
	inFlight    atomic.Bool
}

func newNetpollConn(conn netpoll.Connection) *netpollConn {
	return &netpollConn{conn: conn, local: conn.LocalAddr(), remote: conn.RemoteAddr(), done: make(chan struct{})}
}

func (c *netpollConn) Write(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	if _, err := c.conn.Writer().WriteBinary(p); err != nil {
		return 0, err
	}
	return len(p), c.conn.Writer().Flush()
}

func (c *netpollConn) WriteLease(lease WriteLease) (int, error) {
	retained := lease.Retain()
	body := retained.Bytes()
	if len(body) == 0 {
		retained.Release()
		lease.Release()
		return 0, nil
	}
	if _, err := c.conn.Writer().WriteBinary(body); err != nil {
		retained.Release()
		lease.Release()
		return 0, err
	}
	err := c.conn.Writer().Flush()
	retained.Release()
	lease.Release()
	if err != nil {
		return 0, err
	}
	return len(body), nil
}

func (c *netpollConn) WriteHeaderAndLease(header []byte, lease WriteLease) (int, error) {
	retained := lease.Retain()
	body := retained.Bytes()
	if len(header) == 0 {
		retained.Release()
		return c.WriteLease(lease)
	}
	if _, err := c.conn.Writer().WriteBinary(header); err != nil {
		retained.Release()
		lease.Release()
		return 0, err
	}
	if len(body) > 0 {
		if _, err := c.conn.Writer().WriteBinary(body); err != nil {
			retained.Release()
			lease.Release()
			return 0, err
		}
	}
	err := c.conn.Writer().Flush()
	retained.Release()
	lease.Release()
	if err != nil {
		return 0, err
	}
	return len(header) + len(body), nil
}

func (c *netpollConn) Writev(chunks ...[]byte) (int, error) {
	writer := c.conn.Writer()
	total := 0
	for _, chunk := range chunks {
		if len(chunk) == 0 {
			continue
		}
		n, err := writer.WriteBinary(chunk)
		total += n
		if err != nil {
			return total, err
		}
	}
	if total == 0 {
		return 0, nil
	}
	if err := writer.Flush(); err != nil {
		return total, err
	}
	return total, nil
}

func (c *netpollConn) Peek(_ []byte) []byte {
	reader := c.conn.Reader()
	n := reader.Len()
	if n == 0 {
		return nil
	}
	buf, err := reader.Peek(n)
	if err != nil {
		return nil
	}
	return buf
}

func (c *netpollConn) AcquireRead() ReadLease {
	reader := c.conn.Reader()
	n := reader.Len()
	if n == 0 {
		c.lease.reset(nil, nil)
		return &c.lease
	}
	buf, err := reader.Peek(n)
	if err != nil {
		c.lease.reset(nil, nil)
		return &c.lease
	}
	c.lease.reset(reader, buf)
	return &c.lease
}

func (c *netpollConn) Discard(n int) (int, error) {
	if n <= 0 {
		return 0, nil
	}
	if err := c.conn.Reader().Skip(n); err != nil {
		return 0, err
	}
	return n, nil
}

func (c *netpollConn) PauseRead()  { c.readPaused.Store(true) }
func (c *netpollConn) ResumeRead() { c.readPaused.Store(false) }
func (c *netpollConn) CompleteRequest() {
	c.inFlight.Store(false)
}
func (c *netpollConn) Close() error {
	c.closed.Store(true)
	return c.conn.Close()
}
func (c *netpollConn) Context() any         { return c.ctx }
func (c *netpollConn) SetContext(v any)     { c.ctx = v }
func (c *netpollConn) LocalAddr() net.Addr  { return c.local }
func (c *netpollConn) RemoteAddr() net.Addr { return c.remote }

func (c *netpollConn) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	c.hijacked.Store(true)
	if c.rw == nil {
		c.rw = bufio.NewReadWriter(bufio.NewReader(c.conn), bufio.NewWriter(c.conn))
	}
	return c.conn, c.rw, nil
}

func (c *netpollConn) SetReadHandler(h func(net.Conn, *bufio.ReadWriter) error) {
	c.readHandler = h
	if c.rw == nil {
		c.rw = bufio.NewReadWriter(bufio.NewReader(c.conn), bufio.NewWriter(c.conn))
	}
}

func (c *netpollConn) serve(events Events) error {
	if c.readHandler != nil {
		conn, rw, err := c.Hijack()
		if err != nil {
			return err
		}
		return c.readHandler(conn, rw)
	}
	if c.hijacked.Load() {
		<-c.done
		return nil
	}
	if events.OnData == nil {
		return nil
	}
	if c.readPaused.Load() {
		return nil
	}
	if !c.inFlight.CompareAndSwap(false, true) {
		return nil
	}
	err := events.OnData(c)
	if err != nil {
		c.inFlight.Store(false)
		return err
	}
	if c.readHandler != nil {
		return nil
	}
	if c.hijacked.Load() {
		<-c.done
		return nil
	}
	return nil
}

func (c *netpollConn) fireClose(events Events, err error) {
	if !c.closeOnce.CompareAndSwap(false, true) {
		return
	}
	close(c.done)
	if events.OnClose != nil {
		events.OnClose(c, err)
	}
}

type netpollReadLease struct {
	reader   netpoll.Reader
	buf      []byte
	refCount atomic.Int32
	active   bool
}

func (l *netpollReadLease) reset(reader netpoll.Reader, buf []byte) {
	l.reader = reader
	l.buf = buf
	l.active = reader != nil
	l.refCount.Store(1)
}

func (l *netpollReadLease) Bytes() []byte { return l.buf }

func (l *netpollReadLease) Retain() ReadLease {
	if l.active {
		l.refCount.Add(1)
	}
	return l
}

func (l *netpollReadLease) Release() {
	if !l.active {
		return
	}
	if l.refCount.Add(-1) == 0 {
		_ = l.reader.Release()
		l.active = false
	}
}

type netpollWriteLease struct {
	data []byte
}

var netpollWriteLeasePool = sync.Pool{
	New: func() any {
		return &netpollWriteLease{}
	},
}

func acquireNetpollWriteLease(data []byte) *netpollWriteLease {
	lease := netpollWriteLeasePool.Get().(*netpollWriteLease)
	lease.data = data
	return lease
}

func (l *netpollWriteLease) Bytes() []byte { return l.data }

func (l *netpollWriteLease) Retain() WriteLease {
	return acquireNetpollWriteLease(l.data)
}

func (l *netpollWriteLease) Release() {
	l.data = nil
	netpollWriteLeasePool.Put(l)
}
