package parser

func skipSpaces(b []byte) int {
	for i, c := range b {
		if c != ' ' && c != '\t' {
			return i
		}
	}
	return len(b)
}

func scanToByte(b []byte, target byte) (int, bool) {
	for i, c := range b {
		if c == target {
			return i, true
		}
	}
	return 0, false
}

func parseHTTPVersionDigits(b []byte) (uint8, uint8, bool) {
	if len(b) < 3 || b[1] != '.' {
		return 0, 0, false
	}
	if b[0] < '0' || b[0] > '9' || b[2] < '0' || b[2] > '9' {
		return 0, 0, false
	}
	return b[0] - '0', b[2] - '0', true
}

func responseStatusHasNoBody(status uint16) bool {
	if status >= 100 && status < 200 {
		return true
	}
	return status == 204 || status == 304
}

func parseDecimalInt32(b []byte) (int32, bool) {
	if len(b) == 0 {
		return 0, false
	}

	var n int32
	for _, c := range b {
		if c < '0' || c > '9' {
			return 0, false
		}
		n = n*10 + int32(c-'0')
	}

	return n, true
}

func equalFoldToken(b []byte, s string) bool {
	if len(b) != len(s) {
		return false
	}

	for i := range b {
		c := b[i]
		if 'A' <= c && c <= 'Z' {
			c += 'a' - 'A'
		}
		if c != s[i] {
			return false
		}
	}

	return true
}

func matchMethod(b []byte) (Method, bool) {
	switch {
	case equalFoldToken(b, "get"):
		return GET, true
	case equalFoldToken(b, "head"):
		return HEAD, true
	case equalFoldToken(b, "post"):
		return POST, true
	case equalFoldToken(b, "put"):
		return PUT, true
	case equalFoldToken(b, "delete"):
		return DELETE, true
	case equalFoldToken(b, "connect"):
		return CONNECT, true
	case equalFoldToken(b, "options"):
		return OPTIONS, true
	case equalFoldToken(b, "trace"):
		return TRACE, true
	case equalFoldToken(b, "acl"):
		return ACL, true
	case equalFoldToken(b, "bind"):
		return BIND, true
	case equalFoldToken(b, "copy"):
		return COPY, true
	case equalFoldToken(b, "checkout"):
		return CHECKOUT, true
	case equalFoldToken(b, "lock"):
		return LOCK, true
	case equalFoldToken(b, "unlock"):
		return UNLOCK, true
	case equalFoldToken(b, "link"):
		return LINK, true
	case equalFoldToken(b, "mkcol"):
		return MKCOL, true
	case equalFoldToken(b, "move"):
		return MOVE, true
	case equalFoldToken(b, "mkactivity"):
		return MKACTIVITY, true
	case equalFoldToken(b, "merge"):
		return MERGE, true
	case equalFoldToken(b, "m-search"):
		return MSEARCH, true
	case equalFoldToken(b, "mkcalendar"):
		return MKCALENDAR, true
	case equalFoldToken(b, "notify"):
		return NOTIFY, true
	case equalFoldToken(b, "propfind"):
		return PROPFIND, true
	case equalFoldToken(b, "proppatch"):
		return PROPPATCH, true
	case equalFoldToken(b, "patch"):
		return PATCH, true
	case equalFoldToken(b, "purge"):
		return PURGE, true
	case equalFoldToken(b, "report"):
		return REPORT, true
	case equalFoldToken(b, "rebind"):
		return REBIND, true
	case equalFoldToken(b, "subscribe"):
		return SUBSCRIBE, true
	case equalFoldToken(b, "search"):
		return SEARCH, true
	case equalFoldToken(b, "source"):
		return SOURCE, true
	case equalFoldToken(b, "unsubscribe"):
		return UNSUBSCRIBE, true
	case equalFoldToken(b, "unbind"):
		return UNBIND, true
	case equalFoldToken(b, "unlink"):
		return UNLINK, true
	default:
		return 0, false
	}
}
