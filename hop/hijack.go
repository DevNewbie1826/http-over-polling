package hop

import (
	"bufio"
	"errors"
	"net"
	"net/http"
)

type connHijacker interface {
	Hijack() (net.Conn, *bufio.ReadWriter, error)
	SetReadHandler(func(net.Conn, *bufio.ReadWriter) error)
}

func (h *httpResponseWriter) Flush() {
	if h.hijacked || h.writer == nil {
		return
	}
	if !h.headerWritten {
		_ = h.flushPendingBody(true)
	}
	_ = h.writer.Flush()
}

func (h *httpResponseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	if h.hijacked {
		return nil, nil, errors.New("connection already hijacked")
	}
	if h.conn == nil {
		return nil, nil, errors.New("hijack unavailable")
	}
	hijacker, ok := h.conn.(connHijacker)
	if !ok {
		return nil, nil, errors.New("connection does not support hijack")
	}
	conn, rw, err := hijacker.Hijack()
	if err != nil {
		return nil, nil, err
	}
	h.hijacked = true
	h.writer = nil
	h.chunkWriter = nil
	h.pendingBody = nil
	return conn, rw, nil
}

func (h *httpResponseWriter) SetReadHandler(handler func(net.Conn, *bufio.ReadWriter) error) {
	if h.conn == nil {
		return
	}
	hijacker, ok := h.conn.(connHijacker)
	if !ok {
		return
	}
	hijacker.SetReadHandler(handler)
}

var _ http.Flusher = (*httpResponseWriter)(nil)
var _ http.Hijacker = (*httpResponseWriter)(nil)
