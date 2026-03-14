package hop

import (
	"net/http"

	"github.com/DevNewbie1826/http-over-polling/transport"
)

func ListenAndServe(addr string, handler http.Handler, opts ...transport.Option) error {
	return transport.ListenAndServe(addr, Events(handler), opts...)
}

func Events(handler http.Handler) transport.Events {
	return transport.Events{
		OnOpen: func(conn transport.Conn) {
			conn.SetContext(NewHttpConn(conn, handler))
		},
		OnData: func(conn transport.Conn) error {
			hConn := conn.Context().(*HttpConn)
			return hConn.Serve()
		},
	}
}
