package wsapp

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"time"

	"github.com/DevNewbie1826/http-over-polling/hop"
	"github.com/DevNewbie1826/http-over-polling/transport"
	"github.com/gobwas/ws"
	"github.com/gobwas/ws/wsutil"
	"github.com/gorilla/websocket"
)

type ServerKind string

const (
	ServerKindStd ServerKind = "std"
	ServerKindHop ServerKind = "hop"
)

type Server struct {
	kind            ServerKind
	addr            string
	httpServer      *http.Server
	transportServer *transport.Server
}

type readHandlerSetter interface {
	SetReadHandler(func(net.Conn, *bufio.ReadWriter) error)
}

var gorillaUpgrader = websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}

func NewServer(kind ServerKind, addr, filePath string) *Server {
	handler := NewMux(filePath)
	server := &Server{kind: kind, addr: addr}
	if kind == ServerKindHop {
		server.transportServer = transport.NewServer(hop.Events(handler))
		return server
	}
	server.kind = ServerKindStd
	server.httpServer = &http.Server{Addr: addr, Handler: handler}
	return server
}

func (s *Server) ListenAndServe() error {
	if s.kind == ServerKindHop {
		return s.transportServer.ListenAndServe(s.addr)
	}
	return s.httpServer.ListenAndServe()
}

func (s *Server) Shutdown(ctx context.Context) error {
	if s.kind == ServerKindHop {
		return s.transportServer.Shutdown(ctx)
	}
	return s.httpServer.Shutdown(ctx)
}

func NewMux(filePath string) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprint(w, "Welcome to Hon WebSocket Examples!\nAvailable endpoints:\n1. /ws-gobwas-low    (Low-Level Event-Driven)\n2. /ws-gobwas-high   (High-Level Event-Driven using wsutil)\n3. /ws-gorilla-std   (Standard Loop in Goroutine)\n4. /ws-gorilla-event (Gorilla on Reactor Pattern)\n5. /file             (File Serving)\n6. /sse              (Server-Sent Events)\n")
	})
	mux.HandleFunc("/file", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, filePath)
	})
	mux.HandleFunc("/sse", func(w http.ResponseWriter, r *http.Request) {
		hj, ok := w.(http.Hijacker)
		if !ok {
			http.Error(w, "Streaming not supported", http.StatusInternalServerError)
			return
		}
		conn, rw, err := hj.Hijack()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		defer conn.Close()
		if _, err := fmt.Fprintf(rw, "HTTP/1.1 200 OK\r\nContent-Type: text/event-stream\r\nCache-Control: no-cache\r\nConnection: keep-alive\r\n\r\n"); err != nil {
			return
		}
		if err := rw.Flush(); err != nil {
			return
		}
		ticker := time.NewTicker(1 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-r.Context().Done():
				return
			case t := <-ticker.C:
				if _, err := fmt.Fprintf(rw, "data: Server time is %v\r\n\r\n", t); err != nil {
					return
				}
				if err := rw.Flush(); err != nil {
					return
				}
			}
		}
	})
	mux.HandleFunc("/ws-gobwas-low", func(w http.ResponseWriter, r *http.Request) {
		_, _, _, err := ws.UpgradeHTTP(r, w)
		if err != nil {
			log.Printf("[gobwas-low] upgrade error: %v", err)
			return
		}
		if hijacker, ok := w.(readHandlerSetter); ok {
			hijacker.SetReadHandler(func(c net.Conn, rw *bufio.ReadWriter) error {
				header, err := ws.ReadHeader(rw.Reader)
				if err != nil {
					return err
				}
				payload := make([]byte, header.Length)
				if _, err := io.ReadFull(rw.Reader, payload); err != nil {
					return err
				}
				if header.Masked {
					ws.Cipher(payload, header.Mask, 0)
				}
				switch header.OpCode {
				case ws.OpText, ws.OpBinary:
					respHeader := ws.Header{Fin: true, OpCode: header.OpCode, Length: int64(len(payload))}
					if err := ws.WriteHeader(rw.Writer, respHeader); err != nil {
						return err
					}
					if _, err := rw.Writer.Write(payload); err != nil {
						return err
					}
					return rw.Writer.Flush()
				case ws.OpClose:
					return io.EOF
				}
				return nil
			})
		}
	})
	mux.HandleFunc("/ws-gobwas-high", func(w http.ResponseWriter, r *http.Request) {
		_, _, _, err := ws.UpgradeHTTP(r, w)
		if err != nil {
			log.Printf("[gobwas-high] upgrade error: %v", err)
			return
		}
		if hijacker, ok := w.(readHandlerSetter); ok {
			hijacker.SetReadHandler(func(c net.Conn, rw *bufio.ReadWriter) error {
				msg, op, err := wsutil.ReadClientData(rw)
				if err != nil {
					return err
				}
				if err := wsutil.WriteServerMessage(rw, op, msg); err != nil {
					return err
				}
				return rw.Writer.Flush()
			})
		}
	})
	mux.HandleFunc("/ws-gorilla-std", func(w http.ResponseWriter, r *http.Request) {
		conn, err := gorillaUpgrader.Upgrade(w, r, nil)
		if err != nil {
			log.Printf("[gorilla-std] upgrade error: %v", err)
			return
		}
		go func() {
			defer conn.Close()
			for {
				messageType, message, err := conn.ReadMessage()
				if err != nil {
					return
				}
				if err := conn.WriteMessage(messageType, message); err != nil {
					return
				}
			}
		}()
	})
	mux.HandleFunc("/ws-gorilla-event", func(w http.ResponseWriter, r *http.Request) {
		conn, err := gorillaUpgrader.Upgrade(w, r, nil)
		if err != nil {
			log.Printf("[gorilla-event] upgrade error: %v", err)
			return
		}
		if hijacker, ok := w.(readHandlerSetter); ok {
			hijacker.SetReadHandler(func(_ net.Conn, _ *bufio.ReadWriter) error {
				messageType, message, err := conn.ReadMessage()
				if err != nil {
					return err
				}
				return conn.WriteMessage(messageType, message)
			})
		}
	})
	return mux
}
