package parser

import (
	"bufio"
	"fmt"
	"io"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strconv"
	"unsafe"
)

func ReadRequest(r *bufio.Reader) (*http.Request, error) {
	line, err := readCRLFLine(r)
	if err != nil {
		return nil, err
	}

	requestLine, ok := parseRequestLineBytes(line)
	if !ok {
		return nil, ErrReqMethod
	}

	requestURI := string(requestLine.Target)
	requestURL, err := parseRequestURL(requestLine.Method, requestURI)
	if err != nil {
		return nil, err
	}

	type parsedHeader struct {
		key   string
		value string
	}
	parsedHeaders := make([]parsedHeader, 0, 8)
	var headers http.Header
	meta := messageMeta{kind: REQUEST}
	host := requestURL.Host
	hasConnectionClose := false
	hasConnectionKeepAlive := false

	for {
		line, err = readCRLFLine(r)
		if err != nil {
			return nil, err
		}
		if len(line) == 0 {
			break
		}

		header, ok := parseHeaderLineBytes(line)
		if !ok {
			return nil, ErrHeaderOverflow
		}
		if !applyHeaderMeta(&meta, header) {
			return nil, headerSemanticError(meta, header)
		}
		if equalFoldToken(header.Name, "host") {
			if host == "" {
				host = string(header.Value)
			}
			continue
		}
		if equalFoldToken(header.Name, "connection") {
			if headerValueHasTokenBytes(header.Value, "close") {
				hasConnectionClose = true
			}
			if headerValueHasTokenBytes(header.Value, "keep-alive") {
				hasConnectionKeepAlive = true
			}
		}
		key, ok := canonicalHeaderKey(header.Name)
		if !ok {
			key = canonicalHeaderKeyBytes(header.Name)
		}
		parsedHeaders = append(parsedHeaders, parsedHeader{key: key, value: string(header.Value)})
	}
	if len(parsedHeaders) == 0 {
		headers = make(http.Header)
	} else {
		headers = make(http.Header, len(parsedHeaders))
		firstValues := make([]string, len(parsedHeaders))
		firstValueIdx := 0
		for _, h := range parsedHeaders {
			if existing, exists := headers[h.key]; exists {
				headers[h.key] = append(existing, h.value)
				continue
			}
			firstValues[firstValueIdx] = h.value
			headers[h.key] = firstValues[firstValueIdx : firstValueIdx+1 : firstValueIdx+1]
			firstValueIdx++
		}
	}

	req := &http.Request{
		Method:     requestLine.Method.String(),
		URL:        requestURL,
		Proto:      httpVersionString(requestLine.Major, requestLine.Minor),
		ProtoMajor: int(requestLine.Major),
		ProtoMinor: int(requestLine.Minor),
		Header:     headers,
		Host:       host,
		RequestURI: requestURI,
		Close:      shouldCloseAfterReadConnection(requestLine.Major, requestLine.Minor, hasConnectionClose, hasConnectionKeepAlive),
		Body:       http.NoBody,
	}

	switch decideBodyMode(meta) {
	case bodyModeNone:
		req.ContentLength = 0
	case bodyModeContentLength:
		req.ContentLength = int64(meta.contentLength)
		if meta.contentLength > 0 {
			req.Body = io.NopCloser(io.LimitReader(r, int64(meta.contentLength)))
		}
	case bodyModeChunked:
		req.ContentLength = -1
		req.TransferEncoding = []string{"chunked"}
		req.Body = io.NopCloser(httputil.NewChunkedReader(r))
	default:
		return nil, fmt.Errorf("unsupported request body mode")
	}

	return req, nil
}

func readCRLFLine(r *bufio.Reader) ([]byte, error) {
	part, err := r.ReadSlice('\n')
	if err == nil {
		return trimCRLFLine(part)
	}
	if err != bufio.ErrBufferFull {
		return nil, err
	}

	line := append([]byte(nil), part...)
	for {
		part, err = r.ReadSlice('\n')
		line = append(line, part...)
		if err == bufio.ErrBufferFull {
			continue
		}
		if err != nil {
			return nil, err
		}
		return trimCRLFLine(line)
	}
}

func trimCRLFLine(line []byte) ([]byte, error) {
	if len(line) < 2 || line[len(line)-2] != '\r' || line[len(line)-1] != '\n' {
		return nil, ErrNoEndLF
	}
	if len(line)-2 > int(MaxHeaderSize) {
		return nil, ErrHeaderOverflow
	}
	return line[:len(line)-2], nil
}

func parseRequestURL(method Method, requestURI string) (*url.URL, error) {
	if method == CONNECT && (requestURI == "" || requestURI[0] != '/') {
		return &url.URL{Host: requestURI}, nil
	}
	if len(requestURI) > 0 && requestURI[0] == '/' {
		queryIndex := 0
		hasQuery := false
		for i := 1; i < len(requestURI); i++ {
			switch requestURI[i] {
			case '%', '#':
				goto slowPath
			case '?':
				if !hasQuery {
					queryIndex = i
					hasQuery = true
				}
			}
		}
		if hasQuery {
			return &url.URL{Path: requestURI[:queryIndex], RawQuery: requestURI[queryIndex+1:]}, nil
		}
		return &url.URL{Path: requestURI}, nil
	}

slowPath:
	return url.ParseRequestURI(requestURI)
}

func httpVersionString(major, minor uint8) string {
	if major == 1 {
		switch minor {
		case 0:
			return "HTTP/1.0"
		case 1:
			return "HTTP/1.1"
		}
	}
	return "HTTP/" + strconv.Itoa(int(major)) + "." + strconv.Itoa(int(minor))
}

func shouldCloseAfterReadConnection(major, minor uint8, hasConnectionClose, hasConnectionKeepAlive bool) bool {
	if major == 1 && minor == 0 {
		return !hasConnectionKeepAlive
	}
	return hasConnectionClose
}

func headerValueHasTokenBytes(value []byte, token string) bool {
	start := 0
	for start < len(value) {
		for start < len(value) && (value[start] == ' ' || value[start] == '\t' || value[start] == ',') {
			start++
		}
		end := start
		for end < len(value) && value[end] != ',' {
			end++
		}
		trimmedEnd := end
		for trimmedEnd > start && (value[trimmedEnd-1] == ' ' || value[trimmedEnd-1] == '\t') {
			trimmedEnd--
		}
		if equalFoldToken(value[start:trimmedEnd], token) {
			return true
		}
		start = end + 1
	}
	return false
}

func addParsedHeader(headers http.Header, name, value []byte) http.Header {
	if headers == nil {
		headers = make(http.Header, 4)
	}
	v := string(value)
	if canonical, ok := canonicalHeaderKey(name); ok {
		if existing, exists := headers[canonical]; exists {
			headers[canonical] = append(existing, v)
		} else {
			headers[canonical] = []string{v}
		}
		return headers
	}
	key := canonicalHeaderKeyBytes(name)
	if existing, exists := headers[key]; exists {
		headers[key] = append(existing, v)
	} else {
		headers[key] = []string{v}
	}
	return headers
}

func canonicalHeaderKeyBytes(name []byte) string {
	buf := make([]byte, len(name))
	upper := true
	for i, c := range name {
		if upper {
			if 'a' <= c && c <= 'z' {
				c -= 'a' - 'A'
			}
		} else if 'A' <= c && c <= 'Z' {
			c += 'a' - 'A'
		}
		buf[i] = c
		upper = c == '-'
	}
	return unsafe.String(unsafe.SliceData(buf), len(buf))
}

func canonicalHeaderKey(name []byte) (string, bool) {
	switch {
	case equalFoldToken(name, "connection"):
		return "Connection", true
	case equalFoldToken(name, "content-length"):
		return "Content-Length", true
	case equalFoldToken(name, "transfer-encoding"):
		return "Transfer-Encoding", true
	case equalFoldToken(name, "cache-control"):
		return "Cache-Control", true
	case equalFoldToken(name, "accept"):
		return "Accept", true
	case equalFoldToken(name, "accept-encoding"):
		return "Accept-Encoding", true
	case equalFoldToken(name, "accept-language"):
		return "Accept-Language", true
	case equalFoldToken(name, "user-agent"):
		return "User-Agent", true
	case equalFoldToken(name, "referer"):
		return "Referer", true
	case equalFoldToken(name, "dnt"):
		return "Dnt", true
	default:
		return "", false
	}
}
