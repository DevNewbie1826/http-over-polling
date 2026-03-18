package engine

import (
	"net/http"
	"testing"
	"time"
)

func TestEngine_RequestTimeout_AppliesWhenConfigured(t *testing.T) {
	handlerDone := make(chan struct{}, 1)
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() { handlerDone <- struct{}{} }()
		if _, ok := r.Context().Deadline(); !ok {
			t.Fatalf("expected context deadline to be set")
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	eng := NewEngine(handler, WithRequestTimeout(50*time.Millisecond))
	conn := &MockConnection{}
	conn.fillRequest("GET", "/", "")
	state := NewConnectionState(time.Second)
	defer state.Cancel()

	if err := eng.ServeConn(state, conn); err != nil {
		t.Fatalf("ServeConn() error = %v", err)
	}

	select {
	case <-handlerDone:
	case <-time.After(time.Second):
		t.Fatal("handler did not complete")
	}
}

func TestEngine_RequestTimeout_NotAllocatedWhenDisabled(t *testing.T) {
	handlerDone := make(chan struct{}, 1)
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() { handlerDone <- struct{}{} }()
		if _, ok := r.Context().Deadline(); ok {
			t.Fatalf("did not expect context deadline when request timeout disabled")
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	eng := NewEngine(handler)
	conn := &MockConnection{}
	conn.fillRequest("GET", "/", "")
	state := NewConnectionState(time.Second)
	defer state.Cancel()

	if err := eng.ServeConn(state, conn); err != nil {
		t.Fatalf("ServeConn() error = %v", err)
	}

	select {
	case <-handlerDone:
	case <-time.After(time.Second):
		t.Fatal("handler did not complete")
	}
}

func benchmarkEngineHandleRequestTimeout(b *testing.B, timeout time.Duration) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	eng := NewEngine(handler, WithRequestTimeout(timeout))

	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		conn := &MockConnection{}
		conn.fillRequest("GET", "/", "")
		state := NewConnectionState(time.Second)
		if err := eng.ServeConn(state, conn); err != nil {
			b.Fatalf("ServeConn() error = %v", err)
		}
		state.Cancel()
	}
}

func BenchmarkEngine_HandleRequest_TimeoutDisabled(b *testing.B) {
	benchmarkEngineHandleRequestTimeout(b, 0)
}

func BenchmarkEngine_HandleRequest_TimeoutEnabled(b *testing.B) {
	benchmarkEngineHandleRequestTimeout(b, 50*time.Millisecond)
}
