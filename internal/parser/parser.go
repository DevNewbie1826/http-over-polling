package parser

import (
	"errors"
)

var (
	ErrMethod         = errors.New("http method fail")
	ErrStatusLineHTTP = errors.New("http status line http")
	ErrHTTPVersionNum = errors.New("http version number")
	ErrHeaderOverflow = errors.New("http header overflow")
	ErrNoEndLF        = errors.New("http there is no end symbol")
	ErrChunkSize      = errors.New("http wrong chunk size")
	ErrReqMethod      = errors.New("http request wrong method")
	ErrRequestLineLF  = errors.New("http request line wrong LF")
)

var (
	MaxHeaderSize int32 = 4096
)

const unsetContentLength = -1
const scratchBufferSize = 4096
const maxFastHeaders = 64

type fastPhase uint8

const (
	fastPhaseIdle fastPhase = iota
	fastPhaseRequestLine
	fastPhaseResponseLine
	fastPhaseHeaders
	fastPhaseBody
	fastPhaseChunkedSize
	fastPhaseChunkedData
)

type Parser struct {
	defaultType           ReqOrRsp
	currentType           ReqOrRsp
	phase                 string
	pending               []byte
	lineScratch           [scratchBufferSize]byte
	lineScratchLen        int
	fastHeaders           [maxFastHeaders]headerLine
	fastHeaderCount       int
	fastPhase             fastPhase
	messageStarted        bool
	urlOffset             int
	statusOffset          int
	headerValueOffset     int
	Method                Method
	Major                 uint8
	Minor                 uint8
	MaxHeaderSize         int32
	contentLength         int32
	StatusCode            uint16
	hasContentLength      bool
	hasTransferEncoding   bool
	hasConnectionClose    bool
	hasUpgrade            bool
	hasConnectionUpgrade  bool
	messageCompleteCalled bool

	Upgrade bool

	userData interface{}
}

func New(t ReqOrRsp) *Parser {
	p := &Parser{}
	p.Init(t)
	return p
}

func (p *Parser) Init(t ReqOrRsp) {
	p.defaultType = t
	p.currentType = t
	p.phase = initialPhase(t)
	p.Major = 0
	p.Minor = 0
	p.contentLength = unsetContentLength
	p.MaxHeaderSize = MaxHeaderSize

}

func (p *Parser) ReadyUpgradeData() bool {
	return p.messageCompleteCalled && p.Upgrade
}

func (p *Parser) complete(s *Setting, pos int) {
	p.messageCompleteCalled = true
	if s != nil && s.MessageComplete != nil {
		s.MessageComplete(p, pos)
	}
}

func (p *Parser) SetUserData(d interface{}) {
	p.userData = d
}

func (p *Parser) GetUserData() interface{} {
	return p.userData
}

func (p *Parser) Execute(setting *Setting, buf []byte) (success int, err error) {
	return p.executeClean(setting, buf)
}

func (p *Parser) Reset() {
	p.currentType = p.defaultType
	p.phase = initialPhase(p.defaultType)
	p.pending = nil
	p.lineScratchLen = 0
	p.fastHeaderCount = 0
	p.fastPhase = fastPhaseIdle
	p.messageStarted = false
	p.urlOffset = 0
	p.statusOffset = 0
	p.headerValueOffset = 0
	p.Method = 0
	p.Major = 0
	p.Minor = 0
	p.contentLength = unsetContentLength
	p.StatusCode = 0
	p.hasContentLength = false
	p.hasTransferEncoding = false
	p.hasConnectionClose = false
	p.hasUpgrade = false
	p.hasConnectionUpgrade = false
	p.messageCompleteCalled = false
	p.Upgrade = false
}

func (p *Parser) Status() string {
	return p.phase
}

func (p *Parser) EOF() bool {
	if p.currentType == REQUEST {
		return true
	}

	return p.messageCompleteCalled && len(p.pending) == 0
}

func initialPhase(t ReqOrRsp) string {
	switch t {
	case REQUEST:
		return "startReq"
	case RESPONSE:
		return "startRsp"
	case BOTH:
		return "startReqOrRsp"
	default:
		return "startReqOrRsp"
	}
}
