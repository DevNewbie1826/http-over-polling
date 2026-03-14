package hop

import (
	"sync/atomic"
	"time"
)

var timeValue atomic.Value
var timeRFC1123Value atomic.Value

func init() {
	timeValue.Store(time.Now())
	timeRFC1123Value.Store(string(appendTime(nil, time.Now())))
	go func() {
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		var timeBuf [64]byte
		for t := range ticker.C {
			timeBytes := appendTime(timeBuf[:0], t)
			timeValue.Store(t)
			timeRFC1123Value.Store(string(timeBytes))
		}
	}()
}

func NowRFC1123String() string {
	return timeRFC1123Value.Load().(string)
}

func appendTime(b []byte, t time.Time) []byte {
	const days = "SunMonTueWedThuFriSat"
	const months = "JanFebMarAprMayJunJulAugSepOctNovDec"
	t = t.UTC()
	yy, mm, dd := t.Date()
	hh, mn, ss := t.Clock()
	day := days[3*t.Weekday():]
	mon := months[3*(mm-1):]
	return append(b,
		day[0], day[1], day[2], ',', ' ',
		byte('0'+dd/10), byte('0'+dd%10), ' ',
		mon[0], mon[1], mon[2], ' ',
		byte('0'+yy/1000), byte('0'+(yy/100)%10), byte('0'+(yy/10)%10), byte('0'+yy%10), ' ',
		byte('0'+hh/10), byte('0'+hh%10), ':',
		byte('0'+mn/10), byte('0'+mn%10), ':',
		byte('0'+ss/10), byte('0'+ss%10), ' ',
		'G', 'M', 'T')
}
