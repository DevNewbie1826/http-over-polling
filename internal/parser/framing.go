package parser

type bodyMode uint8

const (
	bodyModeNone bodyMode = iota + 1
	bodyModeContentLength
	bodyModeChunked
	bodyModeEOF
)

type messageMeta struct {
	kind                ReqOrRsp
	statusCode          uint16
	hasContentLength    bool
	contentLength       int32
	hasTransferEncoding bool
}

func decideBodyMode(meta messageMeta) bodyMode {
	if meta.hasTransferEncoding {
		return bodyModeChunked
	}
	if meta.hasContentLength {
		if meta.contentLength == 0 {
			return bodyModeNone
		}
		return bodyModeContentLength
	}
	if meta.kind == RESPONSE && !responseStatusHasNoBody(meta.statusCode) {
		return bodyModeEOF
	}
	return bodyModeNone
}
