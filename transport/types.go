package transport

import (
	"net"
	"time"
)

type ReadLease interface {
	Bytes() []byte
	Retain() ReadLease
	Release()
}

type WriteLease interface {
	Bytes() []byte
	Retain() WriteLease
	Release()
}

type Conn interface {
	Write([]byte) (int, error)
	WriteLease(WriteLease) (int, error)
	WriteHeaderAndLease([]byte, WriteLease) (int, error)
	Writev(...[]byte) (int, error)
	Peek([]byte) []byte
	AcquireRead() ReadLease
	Discard(int) (int, error)
	PauseRead()
	ResumeRead()
	ResumeOnNextRead()
	CompleteRequest()
	Close() error
	Context() any
	SetContext(any)
	LocalAddr() net.Addr
	RemoteAddr() net.Addr
}

type Events struct {
	OnOpen  func(Conn)
	OnData  func(Conn) error
	OnClose func(Conn, error)
}

type options struct {
	readTimeout  time.Duration
	writeTimeout time.Duration
	idleTimeout  time.Duration
}

type Option func(*options)

func WithReadTimeout(d time.Duration) Option {
	return func(o *options) { o.readTimeout = d }
}

func WithWriteTimeout(d time.Duration) Option {
	return func(o *options) { o.writeTimeout = d }
}

func WithIdleTimeout(d time.Duration) Option {
	return func(o *options) { o.idleTimeout = d }
}
