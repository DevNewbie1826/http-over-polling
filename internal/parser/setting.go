package parser

// Setting holds parser callbacks for streaming message events.
type Setting struct {
	MessageBegin    func(*Parser, int)
	URL             func(*Parser, []byte, int)
	Status          func(*Parser, []byte, int)
	HeaderField     func(*Parser, []byte, int)
	HeaderValue     func(*Parser, []byte, int)
	HeadersComplete func(*Parser, int)
	Body            func(*Parser, []byte, int)
	MessageComplete func(*Parser, int)
}

// ReqOrRsp selects request parsing, response parsing, or auto-detect mode.
type ReqOrRsp uint8

const (
	REQUEST ReqOrRsp = iota + 1
	RESPONSE
	BOTH
)
