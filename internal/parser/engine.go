package parser

import (
	"bytes"
	"fmt"
)

type parsedMessage struct {
	kind     ReqOrRsp
	request  requestLine
	response responseLine
	headers  []headerLine
	body     []byte
	mode     bodyMode
	meta     messageMeta
}

func (p *Parser) executeClean(setting *Setting, buf []byte) (int, error) {
	if len(buf) == 0 {
		return 0, nil
	}

	if p.defaultType == REQUEST {
		if n, ok, err := p.tryFastSplitRequestEmpty(setting, buf); ok || err != nil {
			return n, err
		}
		if n, ok, err := p.tryFastRequest(setting, buf); ok || err != nil {
			return n, err
		}
		if n, ok, err := p.tryFastChunkedRequest(setting, buf); ok || err != nil {
			return n, err
		}
		if n, ok, err := p.tryFastSplitRequest(setting, buf); ok || err != nil {
			return n, err
		}
	} else {
		if n, ok, err := p.tryFastSplitResponse(setting, buf); ok || err != nil {
			return n, err
		}
		if n, ok, err := p.tryFastResponse(setting, buf); ok || err != nil {
			return n, err
		}

		if n, ok, err := p.tryFastSplitRequest(setting, buf); ok || err != nil {
			return n, err
		}
		if n, ok, err := p.tryFastRequest(setting, buf); ok || err != nil {
			return n, err
		}
		if n, ok, err := p.tryFastChunkedRequest(setting, buf); ok || err != nil {
			return n, err
		}
	}

	p.pending = append(p.pending, buf...)
	if !p.messageStarted {
		if kind, ok := inferMessageKind(p.defaultType, p.pending); ok {
			p.currentType = kind
		}
		p.messageStarted = true
		if setting.MessageBegin != nil {
			setting.MessageBegin(p, 0)
		}
	}

	if p.currentType == REQUEST {
		p.emitPartialRequestFragments(setting)
	} else if p.currentType == RESPONSE {
		p.emitPartialResponseFragments(setting)
	}
	p.emitPartialHeaderValue(setting, buf)

	result, parseResult := parseMessageDetailed(p.pending, p.currentType)
	if parseResult.err != nil {
		return 0, parseResult.err
	}
	ok := parseResult.ok
	if !ok {
		if n := incompleteConsumedForCurrentState(p); n >= 0 {
			return n, nil
		}
		return len(buf), nil
	}

	consumed := len(buf)
	p.applyParsedMessage(result)
	p.emitParsedMessage(setting, result)
	p.pending = nil
	p.messageStarted = false
	p.urlOffset = 0
	p.statusOffset = 0
	p.headerValueOffset = 0
	return consumed, nil
}

func (p *Parser) tryFastSplitRequestEmpty(setting *Setting, buf []byte) (int, bool, error) {
	if !hasNoCallbacks(setting) || p.defaultType != REQUEST {
		return 0, false, nil
	}
	if p.fastPhase == fastPhaseIdle {
		p.fastPhase = fastPhaseRequestLine
		p.currentType = REQUEST
		p.phase = initialPhase(REQUEST)
	}
	if p.fastPhase == fastPhaseBody {
		n, err := p.consumeFastBody(buf)
		return n, true, err
	}
	if p.fastPhase == fastPhaseChunkedSize || p.fastPhase == fastPhaseChunkedData {
		n, err := p.consumeFastChunked(buf)
		return n, true, err
	}

	consumed := 0
	for consumed < len(buf) {
		idx, ok := scanCRLFBytes(buf[consumed:])
		if !ok {
			if err := p.appendScratch(buf[consumed:]); err != nil {
				return 0, true, err
			}
			return len(buf), true, nil
		}

		lineEnd := consumed + idx
		segment := buf[consumed:lineEnd]
		line := segment
		if p.lineScratchLen != 0 {
			if err := p.appendScratch(segment); err != nil {
				return 0, true, err
			}
			line = p.lineScratch[:p.lineScratchLen]
		}

		next := lineEnd + 2
		if p.fastPhase == fastPhaseRequestLine {
			if err := p.consumeFastRequestLine(line); err != nil {
				return 0, true, err
			}
			p.lineScratchLen = 0
			p.fastPhase = fastPhaseHeaders
			consumed = next
			continue
		}

		if p.fastPhase == fastPhaseHeaders {
			if len(line) == 0 {
				if p.hasTransferEncoding {
					p.fastPhase = fastPhaseChunkedSize
					p.lineScratchLen = 0
					chunkConsumed, err := p.consumeFastChunked(buf[next:])
					if err != nil {
						return 0, true, err
					}
					return next + chunkConsumed, true, nil
				}
				if p.contentLength > 0 {
					p.fastPhase = fastPhaseBody
					p.lineScratchLen = 0
					bodyConsumed, err := p.consumeFastBody(buf[next:])
					if err != nil {
						return 0, true, err
					}
					return next + bodyConsumed, true, nil
				}
				p.phase = "messageDone"
				p.complete(nil, next)
				p.fastPhase = fastPhaseIdle
				p.lineScratchLen = 0
				return len(buf), true, nil
			}
			if err := p.consumeFastHeader(line); err != nil {
				return 0, true, err
			}
			p.lineScratchLen = 0
			consumed = next
			continue
		}
	}

	return len(buf), true, nil
}

func (p *Parser) tryFastSplitResponse(setting *Setting, buf []byte) (int, bool, error) {
	if p.defaultType != RESPONSE {
		return 0, false, nil
	}
	if p.fastPhase == fastPhaseIdle {
		p.fastPhase = fastPhaseResponseLine
		p.currentType = RESPONSE
		p.phase = initialPhase(RESPONSE)
	}

	consumed := 0
	for consumed < len(buf) {
		idx, ok := scanCRLFBytes(buf[consumed:])
		if !ok {
			if err := p.appendScratch(buf[consumed:]); err != nil {
				return 0, true, err
			}
			if p.fastPhase == fastPhaseResponseLine {
				p.emitSplitResponseLineFragment(setting)
			}
			return len(buf), true, nil
		}

		lineEnd := consumed + idx
		segment := buf[consumed:lineEnd]
		var line []byte
		if p.lineScratchLen == 0 {
			line = segment
		} else {
			if err := p.appendScratch(segment); err != nil {
				return 0, true, err
			}
			line = p.lineScratch[:p.lineScratchLen]
		}

		next := lineEnd + 2
		if p.fastPhase == fastPhaseResponseLine {
			parsed, ok := parseResponseLineBytes(line)
			if !ok {
				return 0, true, ErrStatusLineHTTP
			}
			p.Method = 0
			p.Major = parsed.Major
			p.Minor = parsed.Minor
			p.StatusCode = parsed.StatusCode
			if setting != nil {
				if setting.MessageBegin != nil && !p.messageStarted {
					setting.MessageBegin(p, 0)
					p.messageStarted = true
				}
				if setting.Status != nil && p.statusOffset < len(parsed.Reason) {
					setting.Status(p, parsed.Reason[p.statusOffset:], next)
					p.statusOffset = len(parsed.Reason)
				}
			}
			p.lineScratchLen = 0
			p.fastPhase = fastPhaseHeaders
			consumed = next
			continue
		}

		if p.fastPhase == fastPhaseHeaders {
			if len(line) == 0 {
				if !responseStatusHasNoBody(p.StatusCode) {
					return 0, false, nil
				}
				if setting != nil && setting.HeadersComplete != nil {
					setting.HeadersComplete(p, next)
				}
				p.phase = "messageDone"
				p.complete(setting, next)
				p.fastPhase = fastPhaseIdle
				p.lineScratchLen = 0
				return len(buf), true, nil
			}
			if setting != nil {
				h, ok := parseHeaderLineBytes(line)
				if ok {
					if setting.HeaderField != nil {
						setting.HeaderField(p, h.Name, next)
					}
					if setting.HeaderValue != nil {
						setting.HeaderValue(p, h.Value, next)
					}
				}
			}
			p.lineScratchLen = 0
			consumed = next
			continue
		}
	}

	return len(buf), true, nil
}

func (p *Parser) emitSplitResponseLineFragment(setting *Setting) {
	if setting == nil {
		return
	}
	if setting.MessageBegin != nil && !p.messageStarted {
		setting.MessageBegin(p, 0)
		p.messageStarted = true
	}
	if setting.Status == nil {
		return
	}
	line := p.lineScratch[:p.lineScratchLen]
	if len(line) < 13 || !bytes.HasPrefix(line, []byte("HTTP/")) {
		return
	}
	reason := line[13:]
	if p.statusOffset < len(reason) {
		setting.Status(p, reason[p.statusOffset:], len(line))
		p.statusOffset = len(reason)
	}
}

func (p *Parser) tryFastResponse(setting *Setting, buf []byte) (int, bool, error) {
	if p.defaultType != RESPONSE && p.defaultType != BOTH {
		return 0, false, nil
	}
	kind, ok := inferMessageKind(p.defaultType, buf)
	if !ok || kind != RESPONSE {
		return 0, false, nil
	}

	result, ok, err := analyzeFastResponse(buf)
	if err != nil {
		return 0, true, err
	}
	if !ok {
		return 0, false, nil
	}

	p.currentType = RESPONSE
	p.phase = "messageDone"
	p.pending = nil
	p.messageStarted = false
	p.urlOffset = 0
	p.statusOffset = len(result.reason)
	p.headerValueOffset = 0
	p.Method = 0
	p.Major = result.major
	p.Minor = result.minor
	p.StatusCode = result.statusCode
	p.hasContentLength = result.hasContentLength
	p.contentLength = result.contentLength
	p.hasTransferEncoding = false
	p.hasConnectionClose = result.hasConnectionClose
	p.hasUpgrade = false
	p.hasConnectionUpgrade = false
	p.Upgrade = false
	p.messageCompleteCalled = false
	p.emitFastResponse(setting, buf, result)
	return len(buf), true, nil
}

func (p *Parser) emitFastResponse(setting *Setting, buf []byte, result fastResponseResult) {
	if setting != nil && setting.MessageBegin != nil {
		setting.MessageBegin(p, 0)
	}
	if setting != nil && setting.Status != nil && len(result.reason) > 0 {
		setting.Status(p, result.reason, len(buf))
	}
	if setting != nil && (setting.HeaderField != nil || setting.HeaderValue != nil) {
		c := newCursor(buf[result.headersStart:result.headersEnd])
		for c.remaining() > 0 {
			lineEnd, ok := scanCRLF(&c)
			if !ok {
				break
			}
			h, ok := parseHeaderLine(&c)
			if !ok {
				break
			}
			if setting.HeaderField != nil {
				setting.HeaderField(p, h.Name, len(buf))
			}
			if setting.HeaderValue != nil {
				setting.HeaderValue(p, h.Value, len(buf))
			}
			c.advance(lineEnd + 2)
		}
	}
	if setting != nil && setting.HeadersComplete != nil {
		setting.HeadersComplete(p, len(buf))
	}
	if setting != nil && setting.Body != nil && len(result.body) > 0 {
		setting.Body(p, result.body, len(buf))
	}
	p.complete(setting, len(buf))
}

type fastResponseResult struct {
	major              uint8
	minor              uint8
	statusCode         uint16
	reason             []byte
	body               []byte
	bodyMode           bodyMode
	hasContentLength   bool
	contentLength      int32
	hasConnectionClose bool
	headersStart       int
	headersEnd         int
}

func analyzeFastResponse(buf []byte) (fastResponseResult, bool, error) {
	c := newCursor(buf)
	line, ok := parseResponseLine(&c)
	if !ok {
		return fastResponseResult{}, false, nil
	}
	lineEnd, ok := scanCRLF(&c)
	if !ok {
		return fastResponseResult{}, false, nil
	}
	c.advance(lineEnd + 2)
	res := fastResponseResult{
		major:      line.Major,
		minor:      line.Minor,
		statusCode: line.StatusCode,
		reason:     line.Reason,
	}
	headersStart := c.pos
	meta := messageMeta{kind: RESPONSE, statusCode: line.StatusCode}

	for {
		if c.remaining() < 2 {
			return fastResponseResult{}, false, nil
		}
		if b, _ := c.peek(); b == '\r' {
			c.advance(1)
			if b2, ok := c.peek(); !ok || b2 != '\n' {
				return fastResponseResult{}, false, nil
			}
			c.advance(1)
			res.bodyMode = decideBodyMode(meta)
			res.hasContentLength = meta.hasContentLength
			res.contentLength = meta.contentLength
			res.headersStart = headersStart
			res.headersEnd = c.pos - 1
			if res.bodyMode == bodyModeEOF || res.bodyMode == bodyModeChunked {
				return fastResponseResult{}, false, nil
			}
			if res.bodyMode == bodyModeContentLength {
				if c.remaining() != int(meta.contentLength) {
					return fastResponseResult{}, false, nil
				}
				res.body = c.slice(int(meta.contentLength))
				c.advance(int(meta.contentLength))
			}
			if c.remaining() != 0 {
				return fastResponseResult{}, false, nil
			}
			return res, true, nil
		}
		lineEnd, ok := scanCRLF(&c)
		if !ok {
			return fastResponseResult{}, false, nil
		}
		h, ok := parseHeaderLine(&c)
		if !ok {
			return fastResponseResult{}, false, nil
		}
		if equalFoldToken(h.Name, "connection") && equalFoldToken(h.Value, "close") {
			res.hasConnectionClose = true
		}
		if !applyHeaderMeta(&meta, h) {
			return fastResponseResult{}, false, headerSemanticError(meta, h)
		}
		c.advance(lineEnd + 2)
	}
}

func (p *Parser) tryFastSplitRequest(setting *Setting, buf []byte) (int, bool, error) {
	if p.defaultType != REQUEST {
		return 0, false, nil
	}
	emptySetting := hasNoCallbacks(setting)
	if p.fastPhase == fastPhaseIdle {
		p.fastPhase = fastPhaseRequestLine
		p.currentType = REQUEST
		p.phase = initialPhase(REQUEST)
	}

	if p.fastPhase == fastPhaseBody {
		n, err := p.consumeFastBodyWithCallbacks(setting, buf)
		return n, true, err
	}
	if p.fastPhase == fastPhaseChunkedSize || p.fastPhase == fastPhaseChunkedData {
		n, err := p.consumeFastChunkedWithCallbacks(setting, buf)
		return n, true, err
	}

	consumed := 0
	for consumed < len(buf) {
		idx, ok := scanCRLFBytes(buf[consumed:])
		if !ok {
			if err := p.appendScratch(buf[consumed:]); err != nil {
				return 0, true, err
			}
			if p.fastPhase == fastPhaseRequestLine {
				if emptySetting {
					return len(buf), true, nil
				}
				p.emitSplitRequestLineFragment(setting)
				return len(buf), true, nil
			}
			if p.fastPhase == fastPhaseHeaders {
				if emptySetting {
					return len(buf), true, nil
				}
				if n := p.emitSplitHeaderValueFragment(setting, consumed); n >= 0 {
					return n, true, nil
				}
				return len(buf), true, nil
			}
			return len(buf), true, nil
		}

		lineEnd := consumed + idx
		segment := buf[consumed:lineEnd]
		var line []byte
		if p.lineScratchLen == 0 {
			line = segment
		} else {
			if err := p.appendScratch(segment); err != nil {
				return 0, true, err
			}
			line = p.lineScratch[:p.lineScratchLen]
		}

		next := lineEnd + 2
		if p.fastPhase == fastPhaseRequestLine {
			if err := p.consumeFastRequestLine(line); err != nil {
				return 0, true, err
			}
			if !emptySetting {
				if setting.MessageBegin != nil && !p.messageStarted {
					setting.MessageBegin(p, 0)
					p.messageStarted = true
				}
				if setting.URL != nil {
					target := parsedTargetFromLine(line)
					if p.urlOffset < len(target) {
						setting.URL(p, target[p.urlOffset:], next)
						p.urlOffset = len(target)
					}
				}
			}
			p.lineScratchLen = 0
			p.fastPhase = fastPhaseHeaders
			consumed = next
			continue
		}

		if p.fastPhase == fastPhaseHeaders {
			if len(line) == 0 {
				if p.hasTransferEncoding {
					p.fastPhase = fastPhaseChunkedSize
					p.lineScratchLen = 0
					if !emptySetting && setting.HeadersComplete != nil {
						setting.HeadersComplete(p, next)
					}
					chunkConsumed, err := p.consumeFastChunkedWithCallbacks(setting, buf[next:])
					if err != nil {
						return 0, true, err
					}
					return next + chunkConsumed, true, nil
				}
				if p.contentLength > 0 {
					p.fastPhase = fastPhaseBody
					p.lineScratchLen = 0
					if !emptySetting && setting.HeadersComplete != nil {
						setting.HeadersComplete(p, next)
					}
					bodyConsumed, err := p.consumeFastBodyWithCallbacks(setting, buf[next:])
					if err != nil {
						return 0, true, err
					}
					return next + bodyConsumed, true, nil
				}
				if !emptySetting && setting.HeadersComplete != nil {
					setting.HeadersComplete(p, next)
				}
				p.phase = "messageDone"
				p.complete(setting, next)
				p.fastPhase = fastPhaseIdle
				p.lineScratchLen = 0
				return len(buf), true, nil
			}
			if err := p.consumeFastHeader(line); err != nil {
				return 0, true, err
			}
			if !emptySetting {
				h, ok := parseHeaderLineBytes(line)
				if ok {
					if setting.HeaderField != nil {
						setting.HeaderField(p, h.Name, next)
					}
					if setting.HeaderValue != nil {
						if p.headerValueOffset < len(h.Value) {
							setting.HeaderValue(p, h.Value[p.headerValueOffset:], next)
						}
						p.headerValueOffset = 0
					}
				}
			}
			p.lineScratchLen = 0
			consumed = next
			continue
		}
	}

	return len(buf), true, nil
}

func (p *Parser) tryFastChunkedRequest(setting *Setting, buf []byte) (int, bool, error) {
	if p.defaultType != REQUEST {
		return 0, false, nil
	}
	if p.messageStarted || len(p.pending) != 0 || p.fastPhase != fastPhaseIdle {
		return 0, false, nil
	}
	c := newCursor(buf)
	line, ok := parseRequestLine(&c)
	if !ok {
		return 0, false, nil
	}
	lineEnd, ok := scanCRLF(&c)
	if !ok {
		return 0, false, nil
	}
	headersStart := c.pos + lineEnd + 2
	c.advance(lineEnd + 2)
	res := fastRequestResult{kind: REQUEST, method: line.Method, target: line.Target, major: line.Major, minor: line.Minor}
	meta := messageMeta{kind: REQUEST}
	var emittedHeaders [64]headerLine
	emittedHeaderCount := 0
	deferHeaderCallbacks := setting != nil && (setting.HeaderField != nil || setting.HeaderValue != nil)
	for {
		if c.remaining() < 2 {
			return 0, false, nil
		}
		if b, _ := c.peek(); b == '\r' {
			c.advance(1)
			if b2, ok := c.peek(); !ok || b2 != '\n' {
				return 0, false, nil
			}
			headersEnd := c.pos - 1
			c.advance(1)
			if !meta.hasTransferEncoding {
				return 0, false, nil
			}
			res.headersStart = headersStart
			res.headersEnd = headersEnd
			res.bodyMode = bodyModeChunked
			res.hasTransferEncoding = true
			p.applyFastRequestResult(res)
			if setting != nil && setting.MessageBegin != nil {
				setting.MessageBegin(p, 0)
			}
			if setting != nil && setting.URL != nil && len(res.target) > 0 {
				setting.URL(p, res.target, len(buf))
			}
			if deferHeaderCallbacks {
				for i := 0; i < emittedHeaderCount; i++ {
					h := emittedHeaders[i]
					if setting.HeaderField != nil {
						setting.HeaderField(p, h.Name, len(buf))
					}
					if setting.HeaderValue != nil {
						setting.HeaderValue(p, h.Value, len(buf))
					}
				}
			}
			if setting != nil && setting.HeadersComplete != nil {
				setting.HeadersComplete(p, len(buf))
			}
			for {
				size, ok := parseChunkSizeLine(&c)
				if !ok {
					return 0, false, nil
				}
				if size == 0 {
					if c.remaining() < 2 {
						return 0, false, nil
					}
					if b, _ := c.peek(); b != '\r' {
						return 0, false, ErrChunkSize
					}
					c.advance(1)
					if b, ok := c.peek(); !ok || b != '\n' {
						return 0, false, ErrChunkSize
					}
					c.advance(1)
					if c.remaining() != 0 {
						return 0, false, nil
					}
					p.complete(setting, len(buf))
					return len(buf), true, nil
				}
				chunk, ok := consumeChunkData(&c, size)
				if !ok {
					return 0, false, nil
				}
				if setting != nil && setting.Body != nil && len(chunk) > 0 {
					setting.Body(p, chunk, len(buf))
				}
			}
		}
		lineEnd, ok := scanCRLF(&c)
		if !ok {
			return 0, false, nil
		}
		h, ok := parseHeaderLineBytes(c.slice(lineEnd))
		if !ok {
			return 0, false, nil
		}
		if !applyHeaderMeta(&meta, h) {
			return 0, true, headerSemanticError(meta, h)
		}
		if deferHeaderCallbacks {
			if emittedHeaderCount >= len(emittedHeaders) {
				deferHeaderCallbacks = false
			} else {
				emittedHeaders[emittedHeaderCount] = h
				emittedHeaderCount++
			}
		}
		if equalFoldToken(h.Name, "connection") {
			if equalFoldToken(h.Value, "close") {
				res.hasConnectionClose = true
			}
			if equalFoldToken(h.Value, "upgrade") {
				res.hasConnectionUpgrade = true
			}
		}
		if equalFoldToken(h.Name, "upgrade") {
			res.hasUpgrade = true
		}
		c.advance(lineEnd + 2)
	}
}

func (p *Parser) appendScratch(b []byte) error {
	if p.lineScratchLen+len(b) > scratchBufferSize {
		return fmt.Errorf("fast scratch overflow")
	}
	copy(p.lineScratch[p.lineScratchLen:], b)
	p.lineScratchLen += len(b)
	return nil
}

func (p *Parser) consumeFastRequestLine(line []byte) error {
	parsed, ok := parseRequestLineBytes(line)
	if !ok {
		return ErrReqMethod
	}
	p.Method = parsed.Method
	p.Major = parsed.Major
	p.Minor = parsed.Minor
	p.messageStarted = false
	p.phase = "requestLine"
	return nil
}

func parsedTargetFromLine(line []byte) []byte {
	parsed, ok := parseRequestLineBytes(line)
	if !ok {
		return nil
	}
	return parsed.Target
}

func (p *Parser) consumeFastHeader(line []byte) error {
	h, ok := parseHeaderLineBytes(line)
	if !ok {
		return ErrHeaderOverflow
	}
	if equalFoldToken(h.Name, "content-length") {
		if p.hasContentLength {
			return fmt.Errorf("duplicate Content-Length")
		}
		n, ok := parseDecimalInt32(h.Value)
		if !ok {
			return fmt.Errorf("invalid Content-Length: %q", h.Value)
		}
		p.hasContentLength = true
		p.contentLength = n
		return nil
	}
	if equalFoldToken(h.Name, "transfer-encoding") && equalFoldToken(h.Value, "chunked") {
		p.hasTransferEncoding = true
		return nil
	}
	if equalFoldToken(h.Name, "connection") {
		if equalFoldToken(h.Value, "close") {
			p.hasConnectionClose = true
		}
		if equalFoldToken(h.Value, "upgrade") {
			p.hasConnectionUpgrade = true
		}
		return nil
	}
	if equalFoldToken(h.Name, "upgrade") {
		p.hasUpgrade = true
	}
	return nil
}

func (p *Parser) consumeFastBody(buf []byte) (int, error) {
	return p.consumeFastBodyWithCallbacks(nil, buf)
}

func (p *Parser) consumeFastBodyWithCallbacks(setting *Setting, buf []byte) (int, error) {
	if p.contentLength <= 0 {
		p.phase = "messageDone"
		p.complete(setting, 0)
		p.fastPhase = fastPhaseIdle
		return 0, nil
	}
	toConsume := len(buf)
	if int32(toConsume) > p.contentLength {
		toConsume = int(p.contentLength)
	}
	if setting != nil && setting.Body != nil && toConsume > 0 {
		setting.Body(p, buf[:toConsume], toConsume)
	}
	p.contentLength -= int32(toConsume)
	if p.contentLength == 0 {
		p.phase = "messageDone"
		p.complete(setting, toConsume)
		p.fastPhase = fastPhaseIdle
	}
	return toConsume, nil
}

func (p *Parser) emitSplitRequestLineFragment(setting *Setting) {
	if setting == nil {
		return
	}
	if setting.MessageBegin != nil && !p.messageStarted {
		setting.MessageBegin(p, 0)
		p.messageStarted = true
	}
	if setting.URL == nil {
		return
	}
	line := p.lineScratch[:p.lineScratchLen]
	firstSpace, ok := scanToByte(line, ' ')
	if !ok || len(line) <= firstSpace+1 {
		return
	}
	target := line[firstSpace+1:]
	if p.urlOffset < len(target) {
		setting.URL(p, target[p.urlOffset:], len(line))
		p.urlOffset = len(target)
	}
}

func (p *Parser) emitSplitHeaderValueFragment(setting *Setting, consumed int) int {
	if setting == nil || setting.HeaderValue == nil {
		return -1
	}
	line := p.lineScratch[:p.lineScratchLen]
	sep, ok := scanToByte(line, ':')
	if !ok {
		return -1
	}
	value := line[sep+1:]
	trim := skipSpaces(value)
	value = value[trim:]
	if p.headerValueOffset < len(value) {
		setting.HeaderValue(p, value[p.headerValueOffset:], len(line))
		p.headerValueOffset = len(value)
	}
	return consumed + sep + 1 + trim
}

func (p *Parser) consumeFastChunked(buf []byte) (int, error) {
	return p.consumeFastChunkedWithCallbacks(nil, buf)
}

func (p *Parser) consumeFastChunkedWithCallbacks(setting *Setting, buf []byte) (int, error) {
	consumed := 0
	for consumed < len(buf) {
		if p.fastPhase == fastPhaseChunkedData {
			toConsume := len(buf) - consumed
			if int32(toConsume) > p.contentLength {
				toConsume = int(p.contentLength)
			}
			if setting != nil && setting.Body != nil && toConsume > 0 {
				setting.Body(p, buf[consumed:consumed+toConsume], consumed+toConsume)
			}
			p.contentLength -= int32(toConsume)
			consumed += toConsume
			if p.contentLength == 0 {
				p.fastPhase = fastPhaseChunkedSize
				if len(buf)-consumed < 2 {
					if err := p.appendScratch(buf[consumed:]); err != nil {
						return 0, err
					}
					return len(buf), nil
				}
				if buf[consumed] != '\r' || buf[consumed+1] != '\n' {
					return 0, ErrChunkSize
				}
				consumed += 2
			}
			continue
		}

		idx, ok := scanCRLFBytes(buf[consumed:])
		if !ok {
			if err := p.appendScratch(buf[consumed:]); err != nil {
				return 0, err
			}
			return len(buf), nil
		}

		lineEnd := consumed + idx
		segment := buf[consumed:lineEnd]
		var line []byte
		if p.lineScratchLen == 0 {
			line = segment
		} else {
			if err := p.appendScratch(segment); err != nil {
				return 0, err
			}
			line = p.lineScratch[:p.lineScratchLen]
		}

		size, ok := parseChunkSizeLineBytes(line)
		if !ok {
			return 0, ErrChunkSize
		}
		p.lineScratchLen = 0
		consumed = lineEnd + 2
		if size == 0 {
			if len(buf)-consumed < 2 {
				if err := p.appendScratch(buf[consumed:]); err != nil {
					return 0, err
				}
				return len(buf), nil
			}
			if buf[consumed] != '\r' || buf[consumed+1] != '\n' {
				return 0, ErrChunkSize
			}
			consumed += 2
			p.phase = "messageDone"
			p.complete(setting, consumed)
			p.fastPhase = fastPhaseIdle
			return consumed, nil
		}
		p.contentLength = size
		p.fastPhase = fastPhaseChunkedData
	}

	return consumed, nil
}

type fastRequestResult struct {
	kind                 ReqOrRsp
	method               Method
	target               []byte
	major                uint8
	minor                uint8
	body                 []byte
	bodyStart            int
	bodyMode             bodyMode
	hasContentLength     bool
	contentLength        int32
	hasTransferEncoding  bool
	hasConnectionClose   bool
	hasConnectionUpgrade bool
	hasUpgrade           bool
	headersStart         int
	headersEnd           int
}

func (p *Parser) tryFastRequest(setting *Setting, buf []byte) (int, bool, error) {
	if p.messageStarted || len(p.pending) != 0 {
		return 0, false, nil
	}

	kind, ok := inferMessageKind(p.defaultType, buf)
	if !ok || kind != REQUEST {
		return 0, false, nil
	}

	var captured *[maxFastHeaders]headerLine
	var capturedCount *int
	p.fastHeaderCount = 0
	if setting != nil && (setting.HeaderField != nil || setting.HeaderValue != nil) {
		captured = &p.fastHeaders
		capturedCount = &p.fastHeaderCount
	}

	result, ok, err := analyzeFastRequestCaptured(buf, captured, capturedCount)
	if err != nil {
		return 0, true, err
	}
	if !ok {
		p.fastHeaderCount = 0
		return 0, false, nil
	}

	p.applyFastRequestResult(result)
	p.emitFastRequest(setting, buf, result)
	return len(buf), true, nil
}

func analyzeFastRequest(buf []byte) (fastRequestResult, bool, error) {
	return analyzeFastRequestCaptured(buf, nil, nil)
}

func analyzeFastRequestCaptured(buf []byte, captured *[maxFastHeaders]headerLine, capturedCount *int) (fastRequestResult, bool, error) {
	c := newCursor(buf)
	line, ok := parseRequestLine(&c)
	if !ok {
		return fastRequestResult{}, false, nil
	}
	lineEnd, ok := scanCRLF(&c)
	if !ok {
		return fastRequestResult{}, false, nil
	}
	headersStart := c.pos + lineEnd + 2
	c.advance(lineEnd + 2)

	res := fastRequestResult{
		kind:   REQUEST,
		method: line.Method,
		target: line.Target,
		major:  line.Major,
		minor:  line.Minor,
	}
	meta := messageMeta{kind: REQUEST}

	for {
		if c.remaining() < 2 {
			return fastRequestResult{}, false, nil
		}
		if b, _ := c.peek(); b == '\r' {
			c.advance(1)
			if b2, ok := c.peek(); !ok || b2 != '\n' {
				return fastRequestResult{}, false, nil
			}
			headersEnd := c.pos - 1
			c.advance(1)
			res.headersStart = headersStart
			res.headersEnd = headersEnd
			res.bodyMode = decideBodyMode(meta)
			res.hasContentLength = meta.hasContentLength
			res.contentLength = meta.contentLength
			res.hasTransferEncoding = meta.hasTransferEncoding
			res.bodyStart = c.pos
			if res.bodyMode == bodyModeChunked {
				bodyCursor := c
				for {
					size, ok := parseChunkSizeLine(&bodyCursor)
					if !ok {
						return fastRequestResult{}, false, nil
					}
					if size == 0 {
						if bodyCursor.remaining() < 2 {
							return fastRequestResult{}, false, nil
						}
						if b, _ := bodyCursor.peek(); b != '\r' {
							return fastRequestResult{}, false, ErrChunkSize
						}
						bodyCursor.advance(1)
						if b, ok := bodyCursor.peek(); !ok || b != '\n' {
							return fastRequestResult{}, false, ErrChunkSize
						}
						bodyCursor.advance(1)
						if bodyCursor.remaining() != 0 {
							return fastRequestResult{}, false, nil
						}
						return res, true, nil
					}
					if _, ok := consumeChunkData(&bodyCursor, size); !ok {
						return fastRequestResult{}, false, nil
					}
				}
			}
			if res.bodyMode == bodyModeEOF {
				return fastRequestResult{}, false, nil
			}
			if res.bodyMode == bodyModeContentLength {
				if c.remaining() != int(meta.contentLength) {
					return fastRequestResult{}, false, nil
				}
				res.body = c.slice(int(meta.contentLength))
				c.advance(int(meta.contentLength))
			}
			if c.remaining() != 0 {
				return fastRequestResult{}, false, nil
			}
			return res, true, nil
		}

		lineEnd, ok := scanCRLF(&c)
		if !ok {
			return fastRequestResult{}, false, nil
		}
		h, ok := parseHeaderLineBytes(c.slice(lineEnd))
		if !ok {
			return fastRequestResult{}, false, nil
		}
		if !applyHeaderMeta(&meta, h) {
			return fastRequestResult{}, false, headerSemanticError(meta, h)
		}
		if capturedCount != nil {
			if *capturedCount >= 0 {
				if *capturedCount < len(captured) {
					captured[*capturedCount] = h
					*capturedCount++
				} else {
					*capturedCount = -1
				}
			}
		}
		if equalFoldToken(h.Name, "connection") {
			if equalFoldToken(h.Value, "close") {
				res.hasConnectionClose = true
			}
			if equalFoldToken(h.Value, "upgrade") {
				res.hasConnectionUpgrade = true
			}
		}
		if equalFoldToken(h.Name, "upgrade") {
			res.hasUpgrade = true
		}
		c.advance(lineEnd + 2)
	}
}

func (p *Parser) applyFastRequestResult(result fastRequestResult) {
	p.currentType = result.kind
	p.phase = "messageDone"
	p.pending = nil
	p.messageStarted = false
	p.urlOffset = len(result.target)
	p.statusOffset = 0
	p.headerValueOffset = 0
	p.Method = result.method
	p.Major = result.major
	p.Minor = result.minor
	p.StatusCode = 0
	p.hasContentLength = result.hasContentLength
	p.contentLength = result.contentLength
	p.hasTransferEncoding = result.hasTransferEncoding
	p.hasConnectionClose = result.hasConnectionClose
	p.hasUpgrade = result.hasUpgrade
	p.hasConnectionUpgrade = result.hasConnectionUpgrade
	p.Upgrade = false
	if p.hasUpgrade && p.hasConnectionUpgrade {
		p.Upgrade = true
	} else {
		p.Upgrade = p.Method == CONNECT
	}
	p.messageCompleteCalled = false
	if result.bodyMode == bodyModeNone {
		p.contentLength = unsetContentLength
	}
}

func (p *Parser) emitFastRequest(setting *Setting, buf []byte, result fastRequestResult) {
	at := len(buf)
	messageBegin := setting.MessageBegin
	urlCallback := setting.URL
	headerFieldCallback := setting.HeaderField
	headerValueCallback := setting.HeaderValue
	headersComplete := setting.HeadersComplete
	bodyCallback := setting.Body
	messageComplete := setting.MessageComplete
	if messageBegin != nil {
		messageBegin(p, 0)
	}
	if urlCallback != nil && len(result.target) > 0 {
		urlCallback(p, result.target, at)
	}
	if result.headersStart < result.headersEnd && (headerFieldCallback != nil || headerValueCallback != nil) {
		if p.fastHeaderCount > 0 {
			if headerFieldCallback != nil && headerValueCallback != nil {
				for i := 0; i < p.fastHeaderCount; i++ {
					h := p.fastHeaders[i]
					headerFieldCallback(p, h.Name, at)
					headerValueCallback(p, h.Value, at)
				}
			} else if headerFieldCallback != nil {
				for i := 0; i < p.fastHeaderCount; i++ {
					headerFieldCallback(p, p.fastHeaders[i].Name, at)
				}
			} else {
				for i := 0; i < p.fastHeaderCount; i++ {
					headerValueCallback(p, p.fastHeaders[i].Value, at)
				}
			}
		} else {
			c := newCursor(buf[result.headersStart:result.headersEnd])
			for c.remaining() > 0 {
				lineEnd, ok := scanCRLF(&c)
				if !ok {
					break
				}
				h, ok := parseHeaderLine(&c)
				if !ok {
					break
				}
				if headerFieldCallback != nil {
					headerFieldCallback(p, h.Name, at)
				}
				if headerValueCallback != nil {
					headerValueCallback(p, h.Value, at)
				}
				c.advance(lineEnd + 2)
			}
		}
	}
	if headersComplete != nil {
		headersComplete(p, at)
	}
	if result.bodyMode == bodyModeChunked {
		c := newCursor(buf[result.bodyStart:])
		for {
			size, ok := parseChunkSizeLine(&c)
			if !ok {
				break
			}
			if size == 0 {
				if c.remaining() >= 2 {
					c.advance(2)
				}
				break
			}
			chunk, ok := consumeChunkData(&c, size)
			if !ok {
				break
			}
			if bodyCallback != nil && len(chunk) > 0 {
				bodyCallback(p, chunk, at)
			}
		}
		p.messageCompleteCalled = true
		if messageComplete != nil {
			messageComplete(p, at)
		}
		return
	}
	if bodyCallback != nil && len(result.body) > 0 {
		bodyCallback(p, result.body, at)
	}
	p.messageCompleteCalled = true
	if messageComplete != nil {
		messageComplete(p, at)
	}
}

func hasNoCallbacks(setting *Setting) bool {
	if setting == nil {
		return true
	}
	return setting.MessageBegin == nil &&
		setting.URL == nil &&
		setting.Status == nil &&
		setting.HeaderField == nil &&
		setting.HeaderValue == nil &&
		setting.HeadersComplete == nil &&
		setting.Body == nil &&
		setting.MessageComplete == nil
}

func inferMessageKind(defaultKind ReqOrRsp, pending []byte) (ReqOrRsp, bool) {
	if defaultKind == REQUEST || defaultKind == RESPONSE {
		return defaultKind, true
	}
	for _, b := range pending {
		if b == '\r' || b == '\n' {
			continue
		}
		if len(pending) >= 5 && bytes.HasPrefix(pending, []byte("HTTP/")) {
			return RESPONSE, true
		}
		if b != 'H' || len(pending) >= 5 {
			return REQUEST, true
		}
		return 0, false
	}
	return 0, false
}

func (p *Parser) emitPartialRequestFragments(setting *Setting) {
	lineEnd, ok := scanCRLFBytes(p.pending)
	line := p.pending
	if ok {
		line = p.pending[:lineEnd]
	}
	firstSpace, ok := scanToByte(line, ' ')
	if !ok || len(line) <= firstSpace+1 {
		return
	}
	secondSpace, ok := scanToByte(line[firstSpace+1:], ' ')
	if ok {
		secondSpace += firstSpace + 1
		if setting.URL != nil && p.urlOffset < secondSpace-(firstSpace+1) {
			part := line[firstSpace+1+p.urlOffset : secondSpace]
			if len(part) > 0 {
				setting.URL(p, part, len(p.pending))
				p.urlOffset = secondSpace - (firstSpace + 1)
			}
		}
		return
	}
	if setting.URL != nil {
		part := line[firstSpace+1:]
		if p.urlOffset < len(part) {
			fragment := part[p.urlOffset:]
			if len(fragment) > 0 {
				setting.URL(p, fragment, len(p.pending))
				p.urlOffset = len(part)
			}
		}
	}
}

func (p *Parser) emitPartialResponseFragments(setting *Setting) {
	lineEnd, ok := scanCRLFBytes(p.pending)
	line := p.pending
	if ok {
		line = p.pending[:lineEnd]
	}
	if len(line) < 13 || !bytes.HasPrefix(line, []byte("HTTP/")) {
		return
	}
	reason := line[13:]
	if setting.Status != nil && p.statusOffset < len(reason) {
		fragment := reason[p.statusOffset:]
		if len(fragment) > 0 {
			setting.Status(p, fragment, len(p.pending))
			p.statusOffset = len(reason)
		}
	}
}

func (p *Parser) emitPartialHeaderValue(setting *Setting, buf []byte) {
	lineEnd, ok := scanCRLFBytes(p.pending)
	if ok {
		_ = lineEnd
	}
	lastCRLF := bytes.LastIndex(p.pending, []byte("\r\n"))
	start := 0
	if lastCRLF >= 0 {
		start = lastCRLF + 2
	}
	line := p.pending[start:]
	if len(line) == 0 || bytes.Contains(line, []byte("\r\n")) {
		return
	}
	sep, ok := scanToByte(line, ':')
	if !ok {
		return
	}
	value := line[sep+1:]
	trim := skipSpaces(value)
	value = value[trim:]
	if setting.HeaderValue != nil && p.headerValueOffset < len(value) {
		fragment := value[p.headerValueOffset:]
		if len(fragment) > 0 {
			setting.HeaderValue(p, fragment, len(p.pending))
			p.headerValueOffset = len(value)
		}
	}
}

func incompleteConsumedForCurrentState(p *Parser) int {
	if p.currentType == REQUEST {
		lineEnd, ok := scanCRLFBytes(p.pending)
		if !ok {
			return len(p.pending)
		}
		pendingHeaders := p.pending[lineEnd+2:]
		lastCRLF := bytes.LastIndex(pendingHeaders, []byte("\r\n"))
		line := pendingHeaders
		if lastCRLF >= 0 {
			line = pendingHeaders[lastCRLF+2:]
		}
		sep, ok := scanToByte(line, ':')
		if ok {
			value := line[sep+1:]
			return len(p.pending) - len(value[skipSpaces(value):])
		}
	}
	return -1
}

func (p *Parser) applyParsedMessage(msg parsedMessage) {
	p.currentType = msg.kind
	if msg.kind == REQUEST {
		p.Method = msg.request.Method
		p.Major = msg.request.Major
		p.Minor = msg.request.Minor
	} else {
		p.Method = 0
		p.Major = msg.response.Major
		p.Minor = msg.response.Minor
		p.StatusCode = msg.response.StatusCode
	}
	p.hasContentLength = msg.meta.hasContentLength
	p.contentLength = msg.meta.contentLength
	p.hasTransferEncoding = msg.meta.hasTransferEncoding
	p.Upgrade = false
	for _, h := range msg.headers {
		if equalFoldToken(h.Name, "connection") {
			if equalFoldToken(h.Value, "close") {
				p.hasConnectionClose = true
			}
			if equalFoldToken(h.Value, "upgrade") {
				p.hasConnectionUpgrade = true
			}
		}
		if equalFoldToken(h.Name, "upgrade") {
			p.hasUpgrade = true
		}
	}
	if p.hasUpgrade && p.hasConnectionUpgrade {
		p.Upgrade = p.currentType == REQUEST || p.StatusCode == 101
	} else {
		p.Upgrade = p.Method == CONNECT
	}
	if msg.mode == bodyModeChunked && len(msg.body) > 0 {
		p.contentLength = int32(len(msg.body))
	}
	if msg.mode == bodyModeNone {
		p.contentLength = unsetContentLength
	}
	if p.Upgrade || msg.mode == bodyModeNone || msg.mode == bodyModeContentLength || msg.mode == bodyModeChunked {
		p.messageCompleteCalled = false
	}
}

func (p *Parser) emitParsedMessage(setting *Setting, msg parsedMessage) {
	if msg.kind == REQUEST && setting.URL != nil {
		target := msg.request.Target
		if p.urlOffset < len(target) {
			setting.URL(p, target[p.urlOffset:], len(p.pending))
		}
	} else if msg.kind == RESPONSE && setting.Status != nil {
		reason := msg.response.Reason
		if p.statusOffset < len(reason) {
			setting.Status(p, reason[p.statusOffset:], len(p.pending))
		}
	}
	for _, h := range msg.headers {
		if setting.HeaderField != nil {
			setting.HeaderField(p, h.Name, len(p.pending))
		}
		if setting.HeaderValue != nil {
			value := h.Value
			if p.headerValueOffset > 0 && bytes.Equal(h.Value, value) {
				if p.headerValueOffset < len(value) {
					setting.HeaderValue(p, value[p.headerValueOffset:], len(p.pending))
				}
				p.headerValueOffset = 0
			} else {
				setting.HeaderValue(p, value, len(p.pending))
			}
		}
	}
	if setting.HeadersComplete != nil {
		setting.HeadersComplete(p, len(p.pending))
	}
	if setting.Body != nil && len(msg.body) > 0 {
		setting.Body(p, msg.body, len(p.pending))
	}
	p.complete(setting, len(p.pending))
}

type messageParseResult struct {
	ok  bool
	err error
}

func parseMessage(buf []byte, kind ReqOrRsp) (parsedMessage, bool) {
	msg, result := parseMessageDetailed(buf, kind)
	return msg, result.ok && result.err == nil
}

func parseMessageDetailed(buf []byte, kind ReqOrRsp) (parsedMessage, messageParseResult) {
	c := newCursor(buf)
	msg := parsedMessage{kind: kind}

	switch kind {
	case REQUEST:
		line, ok := parseRequestLine(&c)
		if !ok {
			return parsedMessage{}, messageParseResult{}
		}
		lineEnd, ok := scanCRLF(&c)
		if !ok {
			return parsedMessage{}, messageParseResult{}
		}
		c.advance(lineEnd + 2)
		msg.request = line
		msg.meta.kind = REQUEST
	case RESPONSE:
		line, ok := parseResponseLine(&c)
		if !ok {
			return parsedMessage{}, messageParseResult{}
		}
		lineEnd, ok := scanCRLF(&c)
		if !ok {
			return parsedMessage{}, messageParseResult{}
		}
		c.advance(lineEnd + 2)
		msg.response = line
		msg.meta.kind = RESPONSE
		msg.meta.statusCode = line.StatusCode
	default:
		return parsedMessage{}, messageParseResult{}
	}

	for {
		if c.remaining() < 2 {
			return parsedMessage{}, messageParseResult{}
		}
		if b, _ := c.peek(); b == '\r' {
			c.advance(1)
			if b2, ok := c.peek(); !ok || b2 != '\n' {
				return parsedMessage{}, messageParseResult{}
			}
			c.advance(1)
			break
		}

		h, ok := parseHeaderLine(&c)
		if !ok {
			return parsedMessage{}, messageParseResult{}
		}
		msg.headers = append(msg.headers, h)
		if !applyHeaderMeta(&msg.meta, h) {
			return parsedMessage{}, messageParseResult{err: headerSemanticError(msg.meta, h)}
		}
		lineEnd, _ := scanCRLF(&c)
		c.advance(lineEnd + 2)
	}

	msg.mode = decideBodyMode(msg.meta)
	if msg.mode == bodyModeContentLength {
		if c.remaining() < int(msg.meta.contentLength) {
			return parsedMessage{}, messageParseResult{}
		}
		msg.body = append([]byte(nil), c.slice(int(msg.meta.contentLength))...)
		c.advance(int(msg.meta.contentLength))
	}
	if msg.mode == bodyModeChunked {
		for {
			size, ok := parseChunkSizeLine(&c)
			if !ok {
				return parsedMessage{}, messageParseResult{}
			}
			if size == 0 {
				if c.remaining() < 2 {
					return parsedMessage{}, messageParseResult{}
				}
				if b, _ := c.peek(); b != '\r' {
					return parsedMessage{}, messageParseResult{}
				}
				c.advance(1)
				if b, ok := c.peek(); !ok || b != '\n' {
					return parsedMessage{}, messageParseResult{}
				}
				c.advance(1)
				break
			}
			chunk, ok := consumeChunkData(&c, size)
			if !ok {
				return parsedMessage{}, messageParseResult{}
			}
			msg.body = append(msg.body, chunk...)
		}
	}

	if c.remaining() != 0 {
		return parsedMessage{}, messageParseResult{}
	}

	return msg, messageParseResult{ok: true}
}

func scanCRLFBytes(b []byte) (int, bool) {
	for i := 0; i+1 < len(b); i++ {
		if b[i] == '\r' && b[i+1] == '\n' {
			return i, true
		}
	}
	return 0, false
}

func validateHeaderSemantics(buf []byte, kind ReqOrRsp) error {
	if _, ok := scanCRLFBytes(buf); !ok {
		return nil
	}
	c := newCursor(buf)
	switch kind {
	case REQUEST:
		if _, ok := parseRequestLine(&c); !ok {
			return nil
		}
		lineEnd, ok := scanCRLF(&c)
		if !ok {
			return nil
		}
		c.advance(lineEnd + 2)
	case RESPONSE:
		if _, ok := parseResponseLine(&c); !ok {
			return nil
		}
		lineEnd, ok := scanCRLF(&c)
		if !ok {
			return nil
		}
		c.advance(lineEnd + 2)
	default:
		return nil
	}
	seenContentLength := false
	for {
		if c.remaining() < 2 {
			return nil
		}
		if b, _ := c.peek(); b == '\r' {
			return nil
		}
		if b, _ := c.peek(); b == '\n' {
			return nil
		}
		lineEnd, ok := scanCRLF(&c)
		if !ok {
			return nil
		}
		h, ok := parseHeaderLine(&c)
		if !ok {
			return nil
		}
		if equalFoldToken(h.Name, "content-length") {
			if seenContentLength {
				return fmt.Errorf("duplicate Content-Length")
			}
			if _, ok := parseDecimalInt32(h.Value); !ok {
				return fmt.Errorf("invalid Content-Length: %q", h.Value)
			}
			seenContentLength = true
		}
		c.advance(lineEnd + 2)
	}
}

func applyHeaderMeta(meta *messageMeta, h headerLine) bool {
	if equalFoldToken(h.Name, "content-length") {
		if meta.hasContentLength {
			return false
		}
		n, ok := parseDecimalInt32(h.Value)
		if !ok {
			return false
		}
		meta.hasContentLength = true
		meta.contentLength = n
		return true
	}
	if equalFoldToken(h.Name, "transfer-encoding") && equalFoldToken(h.Value, "chunked") {
		meta.hasTransferEncoding = true
	}
	return true
}

func headerSemanticError(meta messageMeta, h headerLine) error {
	if equalFoldToken(h.Name, "content-length") {
		if meta.hasContentLength {
			return fmt.Errorf("duplicate Content-Length")
		}
		return fmt.Errorf("invalid Content-Length: %q", h.Value)
	}
	return fmt.Errorf("invalid header semantics")
}
