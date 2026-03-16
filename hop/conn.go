package hop

import (
	"bytes"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"

	"github.com/DevNewbie1826/http-over-polling/internal/bytebufferpool"
	httpparser "github.com/DevNewbie1826/http-over-polling/internal/parser"
	"github.com/DevNewbie1826/http-over-polling/transport"
)

type HttpConn struct {
	conn             transport.Conn
	handler          http.Handler
	remoteAddr       net.Addr
	remoteAddrStr    string
	parser           *httpparser.Parser
	setting          *httpparser.Setting
	request          *http.Request
	parsedURL        url.URL
	asyncWriter      connWriteFlusher
	writer           *httpResponseWriter
	handleErr        error
	requestDone      bool
	requestURI       string
	requestURIBuf    []byte
	connectionVal    string
	connections      [1]string
	contentLength    string
	contentLens      [1]string
	contentType      string
	contentTypes     [1]string
	upgrade          string
	upgrades         [1]string
	trailer          string
	trailers         [1]string
	transferEncoding string
	transferEncs     [1]string
	activeRead       transport.ReadLease
	headerName       string
	headerNameBuf    []byte
	headerVal        string
	headerValBuf     []byte
	bodyView         []byte
	body             CompositeBuffer
	bodyLease        readLeaseBody
}

func NewHttpConn(conn transport.Conn, handler http.Handler) *HttpConn {
	if handler == nil {
		handler = http.DefaultServeMux
	}
	hc := &HttpConn{
		conn:          conn,
		handler:       handler,
		remoteAddr:    conn.RemoteAddr(),
		remoteAddrStr: "",
		parser:        httpparser.New(httpparser.REQUEST),
		request:       &http.Request{Header: make(http.Header), Body: http.NoBody},
		writer:        &httpResponseWriter{},
	}
	hc.asyncWriter.conn = conn
	hc.asyncWriter.buf = bytebufferpool.Get()
	hc.parser.SetUserData(hc)
	hc.setting = hc.newParserSetting()
	return hc
}

func (hc *HttpConn) Close() {}

func (hc *HttpConn) Serve() error {
	for {
		lease := hc.conn.AcquireRead()
		hc.activeRead = lease
		buffer := lease.Bytes()
		if len(buffer) == 0 {
			hc.activeRead = nil
			lease.Release()
			return nil
		}
		window := firstRequestWindow(buffer)
		buffer = buffer[:window]
		hc.handleErr = nil
		hc.requestDone = false
		parsedBytes, err := hc.parser.Execute(hc.setting, buffer)
		hc.activeRead = nil
		lease.Release()
		if err != nil {
			return err
		}
		if hc.handleErr != nil {
			return hc.handleErr
		}
		if parsedBytes > 0 {
			if _, err := hc.conn.Discard(parsedBytes); err != nil {
				return err
			}
		}
		if hc.requestDone {
			hc.conn.CompleteRequest()
		}
		if !hc.requestDone || parsedBytes == 0 {
			return nil
		}
		if len(hc.conn.Peek(nil)) == 0 {
			return nil
		}
	}
}

type readLeaseBody struct {
	view  []byte
	off   int
	lease transport.ReadLease
}

func (b *readLeaseBody) Read(p []byte) (int, error) {
	if b.off >= len(b.view) {
		return 0, io.EOF
	}
	n := copy(p, b.view[b.off:])
	b.off += n
	if b.off >= len(b.view) {
		return n, io.EOF
	}
	return n, nil
}

func (b *readLeaseBody) Close() error {
	if b.lease != nil {
		b.lease.Release()
		b.lease = nil
	}
	return nil
}

type connWriteLease struct {
	data []byte
}

var connWriteLeasePool = sync.Pool{
	New: func() any {
		return &connWriteLease{}
	},
}

func acquireConnWriteLease(data []byte) *connWriteLease {
	lease := connWriteLeasePool.Get().(*connWriteLease)
	lease.data = data
	return lease
}

func (l *connWriteLease) Bytes() []byte { return l.data }
func (l *connWriteLease) Retain() transport.WriteLease {
	return acquireConnWriteLease(l.data)
}
func (l *connWriteLease) Release() {
	l.data = nil
	connWriteLeasePool.Put(l)
}

type connWriteFlusher struct {
	conn  transport.Conn
	buf   *bytebufferpool.ByteBuffer
	lease *connWriteLease
	head  [256]byte
}

func (w *connWriteFlusher) Write(p []byte) (int, error) { return w.buf.Write(p) }
func (w *connWriteFlusher) WriteByte(b byte) error      { return w.buf.WriteByte(b) }
func (w *connWriteFlusher) WriteString(s string) (int, error) {
	return w.buf.WriteString(s)
}
func (w *connWriteFlusher) Flush() error {
	if w.buf == nil || len(w.buf.B) == 0 {
		return nil
	}
	lease := acquireConnWriteLease(w.buf.B)
	_, err := w.conn.WriteLease(lease)
	w.buf.Reset()
	return err
}
func (w *connWriteFlusher) WriteLease(p []byte) (int, error) {
	if len(w.buf.B) > 0 {
		header := w.buf.B
		lease := acquireConnWriteLease(p)
		n, err := w.conn.WriteHeaderAndLease(header, lease)
		w.buf.Reset()
		return n, err
	}
	lease := acquireConnWriteLease(p)
	return w.conn.WriteLease(lease)
}

func (w *connWriteFlusher) WriteHeaderAndLease(header []byte, body []byte) (int, error) {
	if len(w.buf.B) > 0 {
		w.buf.Reset()
	}
	lease := acquireConnWriteLease(body)
	return w.conn.WriteHeaderAndLease(header, lease)
}

func (hc *HttpConn) commitHeader() {
	if hc.headerName == "" && len(hc.headerNameBuf) == 0 {
		return
	}
	name := hc.headerName
	if len(hc.headerNameBuf) != 0 {
		name = string(hc.headerNameBuf)
	}
	value := hc.headerVal
	if len(hc.headerValBuf) != 0 {
		value = string(hc.headerValBuf)
	}
	switch {
	case strings.EqualFold(name, "Host"):
		hc.request.Host = value
	case strings.EqualFold(name, "Connection"):
		if hc.connectionVal == "" {
			hc.connectionVal = value
			if len(hc.headerNameBuf) == 0 && len(hc.headerValBuf) == 0 {
				hc.connections[0] = value
				hc.request.Header["Connection"] = hc.connections[:]
				break
			}
		} else {
			hc.connectionVal += ", " + value
		}
		hc.request.Header.Add(name, value)
	case strings.EqualFold(name, "Content-Length"):
		if hc.contentLength == "" && len(hc.headerNameBuf) == 0 && len(hc.headerValBuf) == 0 {
			hc.contentLength = value
			hc.contentLens[0] = value
			hc.request.Header["Content-Length"] = hc.contentLens[:]
		} else {
			hc.request.Header.Add(name, value)
		}
	case strings.EqualFold(name, "Content-Type"):
		if hc.contentType == "" && len(hc.headerNameBuf) == 0 && len(hc.headerValBuf) == 0 {
			hc.contentType = value
			hc.contentTypes[0] = value
			hc.request.Header["Content-Type"] = hc.contentTypes[:]
		} else {
			hc.request.Header.Add(name, value)
		}
	case strings.EqualFold(name, "Upgrade"):
		if hc.upgrade == "" && len(hc.headerNameBuf) == 0 && len(hc.headerValBuf) == 0 {
			hc.upgrade = value
			hc.upgrades[0] = value
			hc.request.Header["Upgrade"] = hc.upgrades[:]
		} else {
			hc.request.Header.Add(name, value)
		}
	case strings.EqualFold(name, "Trailer"):
		if hc.trailer == "" && len(hc.headerNameBuf) == 0 && len(hc.headerValBuf) == 0 {
			hc.trailer = value
			hc.trailers[0] = value
			hc.request.Header["Trailer"] = hc.trailers[:]
		} else {
			hc.request.Header.Add(name, value)
		}
	case strings.EqualFold(name, "Transfer-Encoding"):
		if hc.transferEncoding == "" {
			hc.transferEncoding = value
		} else {
			hc.transferEncoding += ", " + value
		}
		if len(hc.request.Header["Transfer-Encoding"]) == 0 && len(hc.headerNameBuf) == 0 && len(hc.headerValBuf) == 0 {
			hc.transferEncs[0] = value
			hc.request.Header["Transfer-Encoding"] = hc.transferEncs[:]
		} else {
			hc.request.Header.Add(name, value)
		}
	default:
		hc.request.Header.Add(name, value)
	}
	hc.headerName = ""
	hc.headerNameBuf = hc.headerNameBuf[:0]
	hc.headerVal = ""
	hc.headerValBuf = hc.headerValBuf[:0]
}

func (hc *HttpConn) Handle() error {
	hc.writer.writer = &hc.asyncWriter
	hc.writer.conn = hc.conn
	hc.writer.protoMajor = byte(hc.request.ProtoMajor)
	hc.writer.protoMinor = byte(hc.request.ProtoMinor)
	hc.writer.request = hc.request
	return hc.finishRequest(hc.request, hc.writer)
}

func (hc *HttpConn) finishRequest(req *http.Request, writer *httpResponseWriter) error {
	hc.handler.ServeHTTP(writer, req)
	if req.Body != nil && req.Body != http.NoBody {
		_, _ = io.Copy(io.Discard, req.Body)
		_ = req.Body.Close()
	}
	if err := writer.Close(); err != nil {
		return err
	}
	if req.Close && !writer.hijacked {
		return hc.conn.Close()
	}
	return nil
}

func firstRequestWindow(buffer []byte) int {
	headerEnd := bytes.Index(buffer, []byte("\r\n\r\n"))
	if headerEnd < 0 {
		return len(buffer)
	}
	bodyStart := headerEnd + 4
	transferChunked := false
	contentLength := -1
	for i := 0; i < headerEnd; {
		lineEndRel := bytes.Index(buffer[i:headerEnd], []byte("\r\n"))
		line := buffer[i:headerEnd]
		if lineEndRel >= 0 {
			line = buffer[i : i+lineEndRel]
			i += lineEndRel + 2
		} else {
			i = headerEnd
		}
		key, value, ok := bytes.Cut(line, []byte(":"))
		if !ok {
			continue
		}
		key = trimASCIISpace(key)
		value = trimASCIISpace(value)
		switch {
		case bytes.EqualFold(key, []byte("Transfer-Encoding")):
			if asciiContainsTokenFold(value, []byte("chunked")) {
				transferChunked = true
			}
		case bytes.EqualFold(key, []byte("Content-Length")):
			if n, ok := parsePositiveDecimalBytes(value); ok {
				contentLength = n
			}
		}
	}
	if transferChunked {
		if terminator := bytes.Index(buffer[bodyStart:], []byte("\r\n0\r\n\r\n")); terminator >= 0 {
			return bodyStart + terminator + len("\r\n0\r\n\r\n")
		}
		i := bodyStart
		for {
			lineEnd := bytes.Index(buffer[i:], []byte("\r\n"))
			if lineEnd < 0 {
				return len(buffer)
			}
			sizeLine := trimASCIISpace(buffer[i : i+lineEnd])
			if semi := bytes.IndexByte(sizeLine, ';'); semi >= 0 {
				sizeLine = sizeLine[:semi]
			}
			size, ok := parseHexBytes(trimASCIISpace(sizeLine))
			if !ok {
				return len(buffer)
			}
			i += lineEnd + 2
			if len(buffer)-i < size+2 {
				return len(buffer)
			}
			i += size
			if !bytes.Equal(buffer[i:i+2], []byte("\r\n")) {
				return len(buffer)
			}
			i += 2
			if size == 0 {
				if len(buffer)-i >= 2 && bytes.Equal(buffer[i:i+2], []byte("\r\n")) {
					return i + 2
				}
				trailersEnd := bytes.Index(buffer[i:], []byte("\r\n\r\n"))
				if trailersEnd < 0 {
					return len(buffer)
				}
				return i + trailersEnd + 4
			}
		}
	}
	if contentLength >= 0 {
		end := bodyStart + contentLength
		if end <= len(buffer) {
			return end
		}
		return len(buffer)
	}
	return bodyStart
}

func hasFoldedPrefix(buf []byte, prefix string) bool {
	if len(buf) < len(prefix) {
		return false
	}
	for i := 0; i < len(prefix); i++ {
		if (buf[i] | 0x20) != prefix[i] {
			return false
		}
	}
	return true
}

func trimASCIISpace(buf []byte) []byte {
	start := 0
	for start < len(buf) && (buf[start] == ' ' || buf[start] == '\t') {
		start++
	}
	end := len(buf)
	for end > start && (buf[end-1] == ' ' || buf[end-1] == '\t') {
		end--
	}
	return buf[start:end]
}

func parsePositiveDecimalBytes(buf []byte) (int, bool) {
	if len(buf) == 0 {
		return 0, false
	}
	n := 0
	for _, b := range buf {
		if b < '0' || b > '9' {
			return 0, false
		}
		n = n*10 + int(b-'0')
	}
	return n, true
}

func parseHexBytes(buf []byte) (int, bool) {
	if len(buf) == 0 {
		return 0, false
	}
	n := 0
	for _, b := range buf {
		switch {
		case b >= '0' && b <= '9':
			n = (n << 4) | int(b-'0')
		case b >= 'a' && b <= 'f':
			n = (n << 4) | int(b-'a'+10)
		case b >= 'A' && b <= 'F':
			n = (n << 4) | int(b-'A'+10)
		default:
			return 0, false
		}
	}
	return n, true
}

func asciiContainsTokenFold(value []byte, token []byte) bool {
	for len(value) > 0 {
		for len(value) > 0 && (value[0] == ' ' || value[0] == '\t' || value[0] == ',') {
			value = value[1:]
		}
		if len(value) == 0 {
			return false
		}
		end := bytes.IndexByte(value, ',')
		part := value
		if end >= 0 {
			part = value[:end]
		}
		part = trimASCIISpace(part)
		if bytes.EqualFold(part, token) {
			return true
		}
		if end < 0 {
			return false
		}
		value = value[end+1:]
	}
	return false
}
