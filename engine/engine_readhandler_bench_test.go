package engine

import (
	"bufio"
	"net/http"
	"testing"
	"time"

	"github.com/cloudwego/netpoll"
)

func BenchmarkEngine_ReadHandler_BlockingImpact(b *testing.B) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	eng := NewEngine(handler)

	cases := []struct {
		name  string
		sleep time.Duration
	}{
		{name: "non_blocking", sleep: 0},
		{name: "blocking_1ms", sleep: time.Millisecond},
	}

	for _, tc := range cases {
		tc := tc
		b.Run(tc.name, func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				conn := &MockConnection{}
				state := NewConnectionState(time.Second)
				state.ReadHandler = func(c netpoll.Connection, rw *bufio.ReadWriter) error {
					if tc.sleep > 0 {
						time.Sleep(tc.sleep)
					}
					return nil
				}
				if err := eng.ServeConn(state, conn); err != nil {
					b.Fatalf("ServeConn() error = %v", err)
				}
				state.Cancel()
			}
		})
	}
}

func TestEngine_ReadHandler_ConnectionIsolation(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	eng := NewEngine(handler)

	slowDone := make(chan struct{}, 1)
	fastDone := make(chan struct{}, 1)

	slowConn := &MockConnection{}
	slowState := NewConnectionState(time.Second)
	slowState.ReadHandler = func(c netpoll.Connection, rw *bufio.ReadWriter) error {
		time.Sleep(100 * time.Millisecond)
		return nil
	}

	fastConn := &MockConnection{}
	fastState := NewConnectionState(time.Second)
	fastState.ReadHandler = func(c netpoll.Connection, rw *bufio.ReadWriter) error {
		return nil
	}

	go func() {
		_ = eng.ServeConn(slowState, slowConn)
		slowDone <- struct{}{}
	}()
	go func() {
		_ = eng.ServeConn(fastState, fastConn)
		fastDone <- struct{}{}
	}()

	select {
	case <-fastDone:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("fast connection did not complete in time")
	}

	select {
	case <-slowDone:
	case <-time.After(time.Second):
		t.Fatal("slow connection did not complete in time")
	}

	slowState.Cancel()
	fastState.Cancel()
}
