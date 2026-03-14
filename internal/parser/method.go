package parser

type Method int8

const (
	GET Method = iota + 1
	HEAD
	POST
	PUT
	DELETE
	CONNECT
	OPTIONS
	TRACE
	ACL
	BIND
	COPY
	CHECKOUT
	LOCK
	UNLOCK
	LINK
	MKCOL
	MOVE
	MKACTIVITY
	MERGE
	MSEARCH
	MKCALENDAR
	NOTIFY
	PROPFIND
	PROPPATCH
	PATCH
	PURGE
	REPORT
	REBIND
	SUBSCRIBE
	SEARCH
	SOURCE
	UNSUBSCRIBE
	UNBIND
	UNLINK
)

var methodNames = [...]string{
	GET:         "GET",
	HEAD:        "HEAD",
	POST:        "POST",
	PUT:         "PUT",
	DELETE:      "DELETE",
	CONNECT:     "CONNECT",
	OPTIONS:     "OPTIONS",
	TRACE:       "TRACE",
	ACL:         "ACL",
	BIND:        "BIND",
	COPY:        "COPY",
	CHECKOUT:    "CHECKOUT",
	LOCK:        "LOCK",
	UNLOCK:      "UNLOCK",
	LINK:        "LINK",
	MKCOL:       "MKCOL",
	MOVE:        "MOVE",
	MKACTIVITY:  "MKACTIVITY",
	MERGE:       "MERGE",
	MSEARCH:     "M-SEARCH",
	MKCALENDAR:  "MKCALENDAR",
	NOTIFY:      "NOTIFY",
	PROPFIND:    "PROPFIND",
	PROPPATCH:   "PROPPATCH",
	PATCH:       "PATCH",
	PURGE:       "PURGE",
	REPORT:      "REPORT",
	REBIND:      "REBIND",
	SUBSCRIBE:   "SUBSCRIBE",
	SEARCH:      "SEARCH",
	SOURCE:      "SOURCE",
	UNSUBSCRIBE: "UNSUBSCRIBE",
	UNBIND:      "UNBIND",
	UNLINK:      "UNLINK",
}

func (m Method) String() string {
	if m > 0 && int(m) < len(methodNames) {
		if name := methodNames[m]; name != "" {
			return name
		}
	}
	return "UNKNOWN"
}
