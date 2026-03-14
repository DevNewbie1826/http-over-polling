package main

import (
	"os"
	"strings"
	"testing"
)

func TestMainUsesHopListenAndServe(t *testing.T) {
	data, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	src := string(data)
	if !strings.Contains(src, "hop.ListenAndServe") {
		t.Fatal("main.go must use hop.ListenAndServe for the simple example path")
	}
	forbidden := []string{
		"netpoll.CreateListener",
		"netpoll.NewEventLoop",
		"type netpollConn struct",
		"type netpollReadLease struct",
		"type netpollWriteLease struct",
	}
	for _, needle := range forbidden {
		if strings.Contains(src, needle) {
			t.Fatalf("main.go still contains transport implementation detail %q", needle)
		}
	}
}
