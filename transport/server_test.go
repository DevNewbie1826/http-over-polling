package transport

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"runtime"
	"sync/atomic"
	"testing"
	"time"
)

func TestIdleConnectionsDoNotSpawnPerConnGoroutines(t *testing.T) {
	addr := nextAddr(t)
	accepted := make(chan struct{}, 64)
	server := NewServer(Events{
		OnOpen: func(Conn) { accepted <- struct{}{} },
	})

	go func() {
		_ = server.ListenAndServe(addr)
	}()

	waitForDialTarget(t, addr)
	before := runtime.NumGoroutine()
	const connCount = 32
	conns := make([]net.Conn, 0, connCount)
	for i := 0; i < connCount; i++ {
		conn, err := net.Dial("tcp", "127.0.0.1"+addr)
		if err != nil {
			t.Fatalf("Dial() error = %v", err)
		}
		conns = append(conns, conn)
	}
	for i := 0; i < connCount; i++ {
		select {
		case <-accepted:
		case <-time.After(2 * time.Second):
			t.Fatal("timed out waiting for OnOpen callbacks")
		}
	}
	time.Sleep(200 * time.Millisecond)
	after := runtime.NumGoroutine()
	for _, conn := range conns {
		_ = conn.Close()
	}
	if delta := after - before; delta > connCount/4 {
		t.Fatalf("goroutine delta = %d, expected much less than idle conn count %d", delta, connCount)
	}
}

func TestSetReadHandlerDispatchesFutureReads(t *testing.T) {
	addr := nextAddr(t)
	installed := make(chan struct{}, 1)
	callbackPayload := make(chan string, 1)
	var dispatches atomic.Int32

	server := NewServer(Events{
		OnData: func(conn Conn) error {
			lease := conn.AcquireRead()
			data := append([]byte(nil), lease.Bytes()...)
			lease.Release()
			if len(data) > 0 {
				if _, err := conn.Discard(len(data)); err != nil {
					return err
				}
			}

			if dispatches.Add(1) != 1 {
				return nil
			}

			nc := conn.(*netpollConn)
			nc.SetReadHandler(func(c net.Conn, rw *bufio.ReadWriter) error {
				line, err := rw.ReadString('\n')
				if err != nil {
					return err
				}
				callbackPayload <- line
				return c.Close()
			})
			installed <- struct{}{}
			return nil
		},
	})

	go func() {
		_ = server.ListenAndServe(addr)
	}()

	waitForDialTarget(t, addr)
	conn, err := net.Dial("tcp", "127.0.0.1"+addr)
	if err != nil {
		t.Fatalf("Dial() error = %v", err)
	}
	defer conn.Close()

	if _, err := io.WriteString(conn, "first payload\n"); err != nil {
		t.Fatalf("first WriteString() error = %v", err)
	}
	select {
	case <-installed:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for SetReadHandler installation")
	}

	if _, err := io.WriteString(conn, "second payload\n"); err != nil {
		t.Fatalf("second WriteString() error = %v", err)
	}
	select {
	case got := <-callbackPayload:
		if got != "second payload\n" {
			t.Fatalf("callback payload = %q, want %q", got, "second payload\\n")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for SetReadHandler callback")
	}

	if got := dispatches.Load(); got != 1 {
		t.Fatalf("OnData dispatches = %d, want 1 before read handler takeover", got)
	}
}

func TestHijackWithoutReadHandlerKeepsConnectionOwnedUntilClose(t *testing.T) {
	addr := nextAddr(t)
	hijacked := make(chan net.Conn, 1)
	reentered := make(chan struct{}, 1)
	var dispatches atomic.Int32

	server := NewServer(Events{
		OnData: func(conn Conn) error {
			lease := conn.AcquireRead()
			data := append([]byte(nil), lease.Bytes()...)
			lease.Release()
			if len(data) > 0 {
				if _, err := conn.Discard(len(data)); err != nil {
					return err
				}
			}

			if dispatches.Add(1) != 1 {
				reentered <- struct{}{}
				return nil
			}

			nc := conn.(*netpollConn)
			hijackedConn, _, err := nc.Hijack()
			if err != nil {
				return err
			}
			hijacked <- hijackedConn
			return nil
		},
	})

	go func() {
		_ = server.ListenAndServe(addr)
	}()

	waitForDialTarget(t, addr)
	conn, err := net.Dial("tcp", "127.0.0.1"+addr)
	if err != nil {
		t.Fatalf("Dial() error = %v", err)
	}
	defer conn.Close()

	if _, err := io.WriteString(conn, "first payload\n"); err != nil {
		t.Fatalf("first WriteString() error = %v", err)
	}
	var owned net.Conn
	select {
	case owned = <-hijacked:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for Hijack")
	}

	if _, err := io.WriteString(conn, "second payload\n"); err != nil {
		t.Fatalf("second WriteString() error = %v", err)
	}
	select {
	case <-reentered:
		t.Fatal("OnData re-entered after Hijack without SetReadHandler")
	case <-time.After(200 * time.Millisecond):
	}

	if err := owned.Close(); err != nil {
		t.Fatalf("owned Close() error = %v", err)
	}
	if got := dispatches.Load(); got != 1 {
		t.Fatalf("OnData dispatches = %d, want 1 after Hijack", got)
	}
}

func nextAddr(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen() error = %v", err)
	}
	defer ln.Close()
	return fmt.Sprintf(":%d", ln.Addr().(*net.TCPAddr).Port)
}

func waitForDialTarget(t *testing.T, addr string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		conn, err := net.DialTimeout("tcp", "127.0.0.1"+addr, 50*time.Millisecond)
		if err == nil {
			_ = conn.Close()
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("server at %s did not become ready: %v", addr, err)
		}
		time.Sleep(25 * time.Millisecond)
	}
}
