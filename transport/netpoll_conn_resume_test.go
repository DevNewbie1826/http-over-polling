package transport

import (
	"sync/atomic"
	"testing"
)

func TestNetpollConnResumeOnNextReadWaitsForFreshData(t *testing.T) {
	writer := newCountingNetpollWriterForTest()
	reader := &scriptedNetpollReaderForTest{data: []byte("first")}
	raw := &countingNetpollConnectionForTest{
		writer: writer,
		reader: reader,
		local:  testNetpollAddr("127.0.0.1:8080"),
		remote: testNetpollAddr("127.0.0.1:12345"),
	}
	conn := newNetpollConn(raw)
	var calls atomic.Int32
	events := Events{OnData: func(c Conn) error {
		if calls.Add(1) == 1 {
			lease := c.AcquireRead()
			data := lease.Bytes()
			if len(data) > 0 {
				if _, err := c.Discard(len(data)); err != nil {
					lease.Release()
					return err
				}
			}
			lease.Release()
			c.ResumeOnNextRead()
		}
		return nil
	}}

	if err := conn.serve(events); err != nil {
		t.Fatalf("first serve() error = %v", err)
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("OnData calls after first drain = %d, want 1", got)
	}

	reader.data = nil
	if err := conn.serve(events); err != nil {
		t.Fatalf("second serve() error = %v", err)
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("OnData calls without fresh data = %d, want 1", got)
	}

	reader.data = []byte("second")
	if err := conn.serve(events); err != nil {
		t.Fatalf("third serve() error = %v", err)
	}
	if got := calls.Load(); got != 2 {
		t.Fatalf("OnData calls after fresh data = %d, want 2", got)
	}
}
