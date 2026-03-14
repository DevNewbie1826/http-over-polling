package transport

import (
	"context"

	"github.com/DevNewbie1826/http-over-polling/internal/tcplisten"
	"github.com/cloudwego/netpoll"
)

type ctxKey struct{}

type Server struct {
	events Events
	opts   options
	loop   netpoll.EventLoop
}

func NewServer(events Events, opts ...Option) *Server {
	serverOpts := options{}
	for _, opt := range opts {
		if opt != nil {
			opt(&serverOpts)
		}
	}
	return &Server{events: events, opts: serverOpts}
}

func ListenAndServe(addr string, events Events, opts ...Option) error {
	return NewServer(events, opts...).ListenAndServe(addr)
}

var defaultListenConfig = &tcplisten.Config{
	ReusePort:   true,
	DeferAccept: true,
	FastOpen:    true,
}

func (s *Server) ListenAndServe(addr string) error {
	ln, err := defaultListenConfig.NewListener("tcp", addr)
	if err != nil {
		return err
	}
	loop, err := netpoll.NewEventLoop(func(ctx context.Context, conn netpoll.Connection) error {
		wrapped, _ := ctx.Value(ctxKey{}).(*netpollConn)
		if wrapped == nil {
			wrapped = newNetpollConn(conn)
		}
		return wrapped.serve(s.events)
	},
		netpoll.WithOnPrepare(func(conn netpoll.Connection) context.Context {
			wrapped := newNetpollConn(conn)
			if s.opts.readTimeout > 0 {
				_ = conn.SetReadTimeout(s.opts.readTimeout)
			}
			if s.opts.writeTimeout > 0 {
				_ = conn.SetWriteTimeout(s.opts.writeTimeout)
			}
			if s.opts.idleTimeout > 0 {
				_ = conn.SetIdleTimeout(s.opts.idleTimeout)
			}
			_ = conn.AddCloseCallback(func(netpoll.Connection) error {
				wrapped.fireClose(s.events, nil)
				return nil
			})
			if s.events.OnOpen != nil {
				s.events.OnOpen(wrapped)
			}
			return context.WithValue(context.Background(), ctxKey{}, wrapped)
		}),
		netpoll.WithOnDisconnect(func(ctx context.Context, conn netpoll.Connection) {
			if wrapped, _ := ctx.Value(ctxKey{}).(*netpollConn); wrapped != nil {
				wrapped.fireClose(s.events, nil)
			}
		}),
	)
	if err != nil {
		return err
	}
	s.loop = loop
	return loop.Serve(ln)
}

func (s *Server) Shutdown(ctx context.Context) error {
	if s.loop == nil {
		return nil
	}
	return s.loop.Shutdown(ctx)
}
