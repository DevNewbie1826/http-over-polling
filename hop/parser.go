package hop

import (
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"unsafe"

	httpparser "github.com/DevNewbie1826/http-over-polling/internal/parser"
	"golang.org/x/net/http/httpguts"
)

var emptyRequest = http.Request{}

func resetHttpRequest(req *http.Request) *http.Request {
	header := req.Header
	clear(header)
	*req = emptyRequest
	req.Header = header
	req.Body = http.NoBody
	return req
}

func resetHttpResponseWriter(writer *httpResponseWriter) *httpResponseWriter {
	writer.protoMajor = 0
	writer.protoMinor = 0
	writer.statusCode = 0
	writer.headerWritten = false
	writer.hijacked = false
	writer.conn = nil
	writer.writer = nil
	writer.chunkWriter = nil
	writer.pendingBody = nil
	clear(writer.header)
	return writer
}

func stableRequestURI(dst *[256]byte, raw string) string {
	if len(raw) == 0 {
		return ""
	}
	if len(raw) <= len(dst) {
		n := copy(dst[:], raw)
		return unsafe.String(&dst[0], n)
	}
	return string(append([]byte(nil), raw...))
}

func (hc *HttpConn) newParserSetting() *httpparser.Setting {
	return &httpparser.Setting{
		MessageBegin: func(p *httpparser.Parser, _ int) {
			hc := p.GetUserData().(*HttpConn)
			hc.request = resetHttpRequest(hc.request)
			hc.writer = resetHttpResponseWriter(hc.writer)
			hc.requestURI = ""
			hc.requestURIBuf = hc.requestURIBuf[:0]
			hc.connectionVal = ""
			hc.connections[0] = ""
			hc.contentLength = ""
			hc.contentLens[0] = ""
			hc.contentType = ""
			hc.contentTypes[0] = ""
			hc.upgrade = ""
			hc.upgrades[0] = ""
			hc.trailer = ""
			hc.trailers[0] = ""
			hc.transferEncoding = ""
			hc.transferEncs[0] = ""
			hc.headerName = ""
			hc.headerNameBuf = hc.headerNameBuf[:0]
			hc.headerVal = ""
			hc.headerValBuf = hc.headerValBuf[:0]
			hc.bodyView = nil
			hc.body.Reset()
		},
		URL: func(p *httpparser.Parser, buf []byte, _ int) {
			hc := p.GetUserData().(*HttpConn)
			if len(hc.requestURIBuf) != 0 {
				hc.requestURIBuf = append(hc.requestURIBuf, buf...)
				return
			}
			if hc.requestURI == "" {
				hc.requestURI = bytesToString(buf)
				return
			}
			hc.requestURIBuf = append(hc.requestURIBuf, hc.requestURI...)
			hc.requestURIBuf = append(hc.requestURIBuf, buf...)
			hc.requestURI = ""
		},
		Status: func(*httpparser.Parser, []byte, int) {},
		HeaderField: func(p *httpparser.Parser, buf []byte, _ int) {
			hc := p.GetUserData().(*HttpConn)
			if hc.headerVal != "" || len(hc.headerValBuf) != 0 {
				hc.commitHeader()
			}
			if len(hc.headerNameBuf) != 0 {
				hc.headerNameBuf = append(hc.headerNameBuf, buf...)
				return
			}
			if hc.headerName == "" {
				hc.headerName = bytesToString(buf)
				return
			}
			hc.headerNameBuf = append(hc.headerNameBuf, hc.headerName...)
			hc.headerNameBuf = append(hc.headerNameBuf, buf...)
			hc.headerName = ""
		},
		HeaderValue: func(p *httpparser.Parser, buf []byte, _ int) {
			hc := p.GetUserData().(*HttpConn)
			if len(hc.headerValBuf) != 0 {
				hc.headerValBuf = append(hc.headerValBuf, buf...)
				return
			}
			if hc.headerVal == "" {
				hc.headerVal = bytesToString(buf)
				return
			}
			hc.headerValBuf = append(hc.headerValBuf, hc.headerVal...)
			hc.headerValBuf = append(hc.headerValBuf, buf...)
			hc.headerVal = ""
		},
		HeadersComplete: func(p *httpparser.Parser, _ int) {
			hc := p.GetUserData().(*HttpConn)
			hc.commitHeader()
		},
		Body: func(p *httpparser.Parser, buf []byte, _ int) {
			hc := p.GetUserData().(*HttpConn)
			if hc.body.Len() == 0 && hc.bodyView == nil {
				hc.bodyView = buf
				return
			}
			if hc.bodyView != nil {
				hc.body.WriteClone(hc.bodyView)
				hc.bodyView = nil
			}
			hc.body.WriteClone(buf)
		},
		MessageComplete: func(p *httpparser.Parser, _ int) {
			hc := p.GetUserData().(*HttpConn)
			hc.commitHeader()

			if len(hc.requestURIBuf) != 0 {
				hc.request.RequestURI = string(hc.requestURIBuf)
			} else {
				hc.request.RequestURI = stableRequestURI(&hc.requestURIInline, hc.requestURI)
			}
			hc.request.Method = getMethod(p.Method)
			hc.request.ProtoMajor = int(p.Major)
			hc.request.ProtoMinor = int(p.Minor)
			if p.Major == 1 && p.Minor == 1 {
				hc.request.Proto = "HTTP/1.1"
			} else if p.Major == 1 && p.Minor == 0 {
				hc.request.Proto = "HTTP/1.0"
			} else {
				hc.request.Proto = httpVersionString(p.Major, p.Minor)
			}
			if hc.body.Len() > 0 {
				hc.request.Body = &hc.body
			} else if hc.bodyView != nil {
				body := &hc.bodyLease
				body.view = bodyViewForHandler(hc.bodyView)
				body.off = 0
				body.lease = nil
				if bodyViewUsesReadLease() && hc.activeRead != nil {
					body.lease = hc.activeRead.Retain()
				}
				hc.request.Body = body
				hc.bodyView = nil
			} else {
				hc.request.Body = http.NoBody
			}
			if hasHeaderToken(hc.transferEncoding, "chunked") {
				hc.request.ContentLength = -1
			} else if cl := hc.request.Header.Get("Content-Length"); cl != "" {
				if n, err := strconv.ParseInt(cl, 10, 64); err == nil {
					hc.request.ContentLength = n
				} else {
					hc.request.ContentLength = 0
				}
			} else {
				hc.request.ContentLength = 0
			}

			hc.request.URL = hc.parseRequestURL(hc.request.Method, hc.request.RequestURI)

			if hc.remoteAddrStr == "" && hc.remoteAddr != nil {
				hc.remoteAddrStr = hc.remoteAddr.String()
			}
			hc.request.RemoteAddr = hc.remoteAddrStr
			hc.request.Close = shouldCloseValue(hc.request.ProtoMajor, hc.request.ProtoMinor, hc.connectionVal)
			if err := hc.Handle(); err != nil {
				hc.handleErr = err
				return
			}
			hc.requestDone = true
		},
	}
}

func getMethod(m httpparser.Method) string {
	if name := m.String(); name != "UNKNOWN" {
		return name
	}
	return ""
}

func shouldClose(major, minor int, header http.Header, removeCloseHeader bool) bool {
	if major < 1 {
		return true
	}

	conv := header["Connection"]
	hasClose := httpguts.HeaderValuesContainsToken(conv, "close")
	if major == 1 && minor == 0 {
		return hasClose || !httpguts.HeaderValuesContainsToken(conv, "keep-alive")
	}

	if hasClose && removeCloseHeader {
		header.Del("Connection")
	}

	return hasClose
}

func bytesToString(buf []byte) string {
	return bytesToStringForHandler(buf)
}

func shouldCloseValue(major, minor int, connection string) bool {
	if major < 1 {
		return true
	}
	hasClose := hasHeaderToken(connection, "close")
	if major == 1 && minor == 0 {
		return hasClose || !hasHeaderToken(connection, "keep-alive")
	}
	return hasClose
}

func hasHeaderToken(value string, token string) bool {
	for value != "" {
		start := 0
		for start < len(value) && (value[start] == ' ' || value[start] == '\t' || value[start] == ',') {
			start++
		}
		value = value[start:]
		if value == "" {
			return false
		}
		end := 0
		for end < len(value) && value[end] != ',' {
			end++
		}
		part := strings.TrimSpace(value[:end])
		if strings.EqualFold(part, token) {
			return true
		}
		if end == len(value) {
			return false
		}
		value = value[end+1:]
	}
	return false
}

func httpVersionString(major, minor uint8) string {
	var b [8]byte
	b[0] = 'H'
	b[1] = 'T'
	b[2] = 'T'
	b[3] = 'P'
	b[4] = '/'
	b[5] = '0' + major
	b[6] = '.'
	b[7] = '0' + minor
	return string(b[:])
}

func (hc *HttpConn) parseRequestURL(method string, raw string) *url.URL {
	hc.parsedURL = url.URL{}
	if method == http.MethodConnect && !strings.HasPrefix(raw, "/") {
		hc.parsedURL.Host = raw
		return &hc.parsedURL
	}
	if raw == "" {
		return &hc.parsedURL
	}
	path, query, ok := strings.Cut(raw, "?")
	if !ok {
		hc.parsedURL.Path = raw
		return &hc.parsedURL
	}
	hc.parsedURL.Path = path
	hc.parsedURL.RawQuery = query
	return &hc.parsedURL
}
