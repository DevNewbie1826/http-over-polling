package hop

import (
	"errors"
	"io"
	"net/http"
	"net/http/httputil"
	"strconv"

	"github.com/DevNewbie1826/http-over-polling/transport"
)

var errNilResponseWriter = errors.New("uhttp: nil response writer")

type responseWriter interface {
	io.Writer
	io.ByteWriter
	io.StringWriter
	Flush() error
}

type leaseWriter interface {
	WriteLease([]byte) (int, error)
}

type httpResponseWriter struct {
	protoMajor, protoMinor byte
	statusCode             int
	header                 http.Header
	headerWritten          bool
	hijacked               bool
	request                *http.Request
	conn                   transport.Conn
	writer                 responseWriter
	chunkWriter            io.WriteCloser
	pendingBody            []byte
	pendingInline          [64]byte
}

func (h *httpResponseWriter) Header() http.Header {
	if h.header == nil {
		h.header = make(http.Header)
	}
	return h.header
}

func (h *httpResponseWriter) Write(b []byte) (int, error) {
	if h.writer == nil {
		return 0, errNilResponseWriter
	}
	if !h.headerWritten {
		if h.pendingBody == nil {
			if len(b) <= len(h.pendingInline) {
				h.pendingBody = h.pendingInline[:len(b)]
				copy(h.pendingBody, b)
			} else {
				h.pendingBody = append(h.pendingBody[:0], b...)
			}
			return len(b), nil
		}
		if err := h.flushPendingBody(true); err != nil {
			return 0, err
		}
	}
	if h.chunkWriter != nil {
		return h.chunkWriter.Write(b)
	}
	return h.writer.Write(b)
}

func (h *httpResponseWriter) Close() error {
	if h.hijacked {
		return nil
	}
	if h.writer == nil {
		return errNilResponseWriter
	}
	if !h.headerWritten {
		if err := h.flushPendingBody(false); err != nil {
			return err
		}
	}
	if h.chunkWriter != nil {
		if err := h.chunkWriter.Close(); err != nil {
			return err
		}
		_, _ = h.writer.WriteString("\r\n")
	}
	return h.writer.Flush()
}

func (h *httpResponseWriter) flushPendingBody(forceChunked bool) error {
	h.headerWritten = true
	if h.statusCode == 0 {
		h.statusCode = http.StatusOK
	}
	if len(h.pendingBody) > 0 && !forceChunked && h.canWriteDirectContentLength() {
		if err := h.writeDirectContentLengthResponse(); err != nil {
			return err
		}
		h.pendingBody = nil
		return nil
	}
	if len(h.pendingBody) > 0 && !forceChunked && (h.header == nil || h.header.Get("Content-Length") == "") && (h.header == nil || h.header.Get("Transfer-Encoding") == "") {
		h.Header().Set("Content-Length", strconv.Itoa(len(h.pendingBody)))
	}
	chunked, err := h.writeResponseHeader(h.request, h.writer)
	if err != nil {
		return err
	}
	if chunked {
		h.chunkWriter = httputil.NewChunkedWriter(h.writer)
	}
	if len(h.pendingBody) == 0 {
		return nil
	}
	if h.chunkWriter != nil {
		_, err = h.chunkWriter.Write(h.pendingBody)
	} else {
		_, err = h.writer.Write(h.pendingBody)
	}
	h.pendingBody = nil
	return err
}

func (h *httpResponseWriter) canWriteDirectContentLength() bool {
	if h.writer == nil || len(h.pendingBody) == 0 {
		return false
	}
	if len(h.header) != 0 {
		return false
	}
	return true
}

func (h *httpResponseWriter) writeDirectContentLengthResponse() error {
	text := http.StatusText(h.statusCode)
	if text == "" {
		text = "status code " + strconv.Itoa(h.statusCode)
	}
	if hw, ok := h.writer.(*connWriteFlusher); ok {
		header := hw.head[:0]
		header = append(header, 'H', 'T', 'T', 'P', '/')
		header = append(header, '0'+h.protoMajor, '.', '0'+h.protoMinor, ' ')
		header = strconv.AppendInt(header, int64(h.statusCode), 10)
		header = append(header, ' ')
		header = append(header, text...)
		header = append(header, '\r', '\n')
		if h.request.Close {
			header = append(header, "Connection: close\r\n"...)
		}
		header = append(header, "Content-Type: text/plain; charset=utf-8\r\nDate: "...)
		header = append(header, NowRFC1123String()...)
		header = append(header, "\r\nContent-Length: "...)
		header = strconv.AppendInt(header, int64(len(h.pendingBody)), 10)
		header = append(header, '\r', '\n', '\r', '\n')
		_, err := hw.WriteHeaderAndLease(header, h.pendingBody)
		return err
	}
	if _, err := h.writer.WriteString("HTTP/"); err != nil {
		return err
	}
	if err := h.writer.WriteByte('0' + h.protoMajor); err != nil {
		return err
	}
	if err := h.writer.WriteByte('.'); err != nil {
		return err
	}
	if err := h.writer.WriteByte('0' + h.protoMinor); err != nil {
		return err
	}
	if err := h.writer.WriteByte(' '); err != nil {
		return err
	}
	if err := writeInt(h.writer, h.statusCode); err != nil {
		return err
	}
	if err := h.writer.WriteByte(' '); err != nil {
		return err
	}
	if _, err := h.writer.WriteString(text); err != nil {
		return err
	}
	if _, err := h.writer.WriteString("\r\n"); err != nil {
		return err
	}
	if h.request.Close {
		if _, err := h.writer.WriteString("Connection: close\r\n"); err != nil {
			return err
		}
	}
	if _, err := h.writer.WriteString("Content-Type: text/plain; charset=utf-8\r\n"); err != nil {
		return err
	}
	if _, err := h.writer.WriteString("Date: "); err != nil {
		return err
	}
	if _, err := h.writer.WriteString(NowRFC1123String()); err != nil {
		return err
	}
	if _, err := h.writer.WriteString("\r\nContent-Length: "); err != nil {
		return err
	}
	if err := writeInt(h.writer, len(h.pendingBody)); err != nil {
		return err
	}
	if _, err := h.writer.WriteString("\r\n\r\n"); err != nil {
		return err
	}
	if lw, ok := h.writer.(leaseWriter); ok {
		_, err := lw.WriteLease(h.pendingBody)
		return err
	}
	_, err := h.writer.Write(h.pendingBody)
	return err
}

func (h *httpResponseWriter) WriteHeader(statusCode int) {
	h.statusCode = statusCode
}

func (h *httpResponseWriter) writeResponseHeader(request *http.Request, w responseWriter) (bool, error) {
	text := http.StatusText(h.statusCode)
	if text == "" {
		text = "status code " + strconv.Itoa(h.statusCode)
	}
	if hw, ok := w.(*connWriteFlusher); ok {
		header := hw.buf.B[:0]
		header = append(header, 'H', 'T', 'T', 'P', '/')
		header = append(header, '0'+h.protoMajor, '.', '0'+h.protoMinor, ' ')
		header = strconv.AppendInt(header, int64(h.statusCode), 10)
		header = append(header, ' ')
		header = append(header, text...)
		header = append(header, '\r', '\n')
		if request.Close {
			header = append(header, "Connection: close\r\n"...)
		}
		if h.header == nil || h.header.Get("Content-Type") == "" {
			header = append(header, "Content-Type: text/plain; charset=utf-8\r\n"...)
		}
		if h.header == nil || h.header.Get("Date") == "" {
			header = append(header, "Date: "...)
			header = append(header, NowRFC1123String()...)
			header = append(header, '\r', '\n')
		}
		chunked := false
		if (h.header == nil || h.header.Get("Content-Length") == "") && h.header.Get("Transfer-Encoding") == "" {
			header = append(header, "Transfer-Encoding: chunked\r\n"...)
			chunked = true
		}
		for key, values := range h.header {
			for _, value := range values {
				header = append(header, key...)
				header = append(header, ':', ' ')
				header = append(header, value...)
				header = append(header, '\r', '\n')
			}
		}
		header = append(header, '\r', '\n')
		hw.buf.B = header
		return chunked, nil
	}
	if _, err := w.WriteString("HTTP/"); err != nil {
		return false, err
	}
	if err := w.WriteByte('0' + h.protoMajor); err != nil {
		return false, err
	}
	if err := w.WriteByte('.'); err != nil {
		return false, err
	}
	if err := w.WriteByte('0' + h.protoMinor); err != nil {
		return false, err
	}
	if err := w.WriteByte(' '); err != nil {
		return false, err
	}
	if err := writeInt(w, h.statusCode); err != nil {
		return false, err
	}
	if err := w.WriteByte(' '); err != nil {
		return false, err
	}
	if _, err := w.WriteString(text); err != nil {
		return false, err
	}
	if _, err := w.WriteString("\r\n"); err != nil {
		return false, err
	}
	if request.Close {
		if _, err := w.WriteString("Connection: close\r\n"); err != nil {
			return false, err
		}
	}
	if h.header == nil || h.header.Get("Content-Type") == "" {
		if _, err := w.WriteString("Content-Type: text/plain; charset=utf-8\r\n"); err != nil {
			return false, err
		}
	}
	if h.header == nil || h.header.Get("Date") == "" {
		if _, err := w.WriteString("Date: "); err != nil {
			return false, err
		}
		if _, err := w.WriteString(NowRFC1123String()); err != nil {
			return false, err
		}
		if _, err := w.WriteString("\r\n"); err != nil {
			return false, err
		}
	}
	chunked := false
	if (h.header == nil || h.header.Get("Content-Length") == "") && h.header.Get("Transfer-Encoding") == "" {
		if _, err := w.WriteString("Transfer-Encoding: chunked\r\n"); err != nil {
			return false, err
		}
		chunked = true
	}
	for key, values := range h.header {
		for _, value := range values {
			if _, err := w.WriteString(key); err != nil {
				return false, err
			}
			if _, err := w.WriteString(": "); err != nil {
				return false, err
			}
			if _, err := w.WriteString(value); err != nil {
				return false, err
			}
			if _, err := w.WriteString("\r\n"); err != nil {
				return false, err
			}
		}
	}
	if _, err := w.WriteString("\r\n"); err != nil {
		return false, err
	}
	return chunked, nil
}

func writeInt(w responseWriter, v int) error {
	if v == 0 {
		return w.WriteByte('0')
	}
	var buf [20]byte
	i := len(buf)
	for v > 0 {
		i--
		buf[i] = byte('0' + v%10)
		v /= 10
	}
	for ; i < len(buf); i++ {
		if err := w.WriteByte(buf[i]); err != nil {
			return err
		}
	}
	return nil
}
