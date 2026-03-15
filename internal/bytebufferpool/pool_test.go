package bytebufferpool

import (
	"sync/atomic"
	"testing"
)

func TestPoolGetUsesDefaultSizeForFreshBuffer(t *testing.T) {
	var p Pool
	atomic.StoreUint64(&p.defaultSize, 128)

	b := p.Get()
	if b == nil {
		t.Fatal("Get() returned nil")
	}
	if b.Len() != 0 {
		t.Fatalf("Len() = %d, want 0", b.Len())
	}
	if cap(b.B) != 128 {
		t.Fatalf("cap(B) = %d, want 128", cap(b.B))
	}
}

func TestPoolPutResetsAndReusesSmallBuffer(t *testing.T) {
	var p Pool
	atomic.StoreUint64(&p.maxSize, 64)

	b := &ByteBuffer{B: make([]byte, 0, 16)}
	b.SetString("payload")
	p.Put(b)

	reused := p.Get()
	if reused.Len() != 0 {
		t.Fatalf("Len() = %d, want 0 after reuse", reused.Len())
	}
	if cap(reused.B) != 16 {
		t.Fatalf("cap(B) = %d, want 16", cap(reused.B))
	}
}

func TestPoolPutSkipsOversizedBufferWhenMaxSizeIsSet(t *testing.T) {
	var p Pool
	atomic.StoreUint64(&p.maxSize, 4)

	tooLarge := &ByteBuffer{B: make([]byte, 0, 8)}
	tooLarge.SetString("payload")
	p.Put(tooLarge)

	if got := p.pool.Get(); got != nil {
		t.Fatalf("pool.Get() = %#v, want nil for oversized buffer", got)
	}
}

func TestPoolPutTriggersCalibrationAtThreshold(t *testing.T) {
	var p Pool
	atomic.StoreUint64(&p.calls[index(1)], calibrateCallsThreshold)

	b := &ByteBuffer{B: []byte{'x'}}
	p.Put(b)

	if got := atomic.LoadUint64(&p.defaultSize); got != minSize {
		t.Fatalf("defaultSize = %d, want %d", got, minSize)
	}
	if got := atomic.LoadUint64(&p.maxSize); got != minSize {
		t.Fatalf("maxSize = %d, want %d", got, minSize)
	}
	if got := atomic.LoadUint64(&p.calibrating); got != 0 {
		t.Fatalf("calibrating = %d, want 0 after calibration", got)
	}
	stored, _ := p.pool.Get().(*ByteBuffer)
	if stored == nil {
		t.Fatal("expected calibrated Put() to leave a pooled buffer")
	}
	if stored.Len() != 0 {
		t.Fatalf("stored.Len() = %d, want 0 after Put reset", stored.Len())
	}
}

func TestPackageLevelGetPutResetsBuffer(t *testing.T) {
	oldDefaultPool := defaultPool
	defaultPool = Pool{}
	t.Cleanup(func() {
		defaultPool = oldDefaultPool
	})
	atomic.StoreUint64(&defaultPool.maxSize, 64)

	b := Get()
	if b == nil {
		t.Fatal("Get() returned nil")
	}
	b.SetString("package level test")
	Put(b)

	reused := Get()
	if reused.Len() != 0 {
		t.Fatalf("Len() = %d, want 0 after package-level reuse", reused.Len())
	}
	if cap(reused.B) != cap(b.B) {
		t.Fatalf("cap(B) = %d, want %d from isolated package-level reuse", cap(reused.B), cap(b.B))
	}
}

func TestIndex(t *testing.T) {
	tests := []struct {
		name string
		size int
		want int
	}{
		{name: "zero", size: 0, want: 0},
		{name: "one", size: 1, want: 0},
		{name: "min size", size: minSize, want: 0},
		{name: "min size plus one", size: minSize + 1, want: 1},
		{name: "bucket 6", size: 4096, want: 6},
		{name: "max size", size: maxSize, want: steps - 1},
		{name: "beyond max", size: maxSize + 1, want: steps - 1},
		{name: "negative", size: -1, want: 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := index(tt.size); got != tt.want {
				t.Fatalf("index(%d) = %d, want %d", tt.size, got, tt.want)
			}
		})
	}
}

func TestCallSizesLenLessSwap(t *testing.T) {
	cs := callSizes{
		{calls: 10, size: 64},
		{calls: 20, size: 128},
		{calls: 5, size: 32},
	}

	if got := cs.Len(); got != 3 {
		t.Fatalf("Len() = %d, want 3", got)
	}
	if cs.Less(0, 1) {
		t.Fatal("Less(0, 1) = true, want false")
	}
	if !cs.Less(1, 0) {
		t.Fatal("Less(1, 0) = false, want true")
	}

	cs.Swap(0, 2)
	if cs[0].calls != 5 || cs[2].calls != 10 {
		t.Fatalf("Swap() did not exchange elements: %#v", cs)
	}
}

func TestCalibrateChoosesDefaultAndMaxSizeFromCallDistribution(t *testing.T) {
	var p Pool
	atomic.StoreUint64(&p.calls[0], 100)
	atomic.StoreUint64(&p.calls[1], 90)
	atomic.StoreUint64(&p.calls[2], 5)

	p.calibrate()

	if got := atomic.LoadUint64(&p.defaultSize); got != minSize {
		t.Fatalf("defaultSize = %d, want %d", got, minSize)
	}
	if got := atomic.LoadUint64(&p.maxSize); got != minSize<<1 {
		t.Fatalf("maxSize = %d, want %d", got, minSize<<1)
	}
	if got := atomic.LoadUint64(&p.calibrating); got != 0 {
		t.Fatalf("calibrating = %d, want 0", got)
	}
	for i := 0; i < 3; i++ {
		if got := atomic.LoadUint64(&p.calls[i]); got != 0 {
			t.Fatalf("calls[%d] = %d, want 0 after calibration", i, got)
		}
	}
}

func TestCalibrateReturnsEarlyWhenAlreadyCalibrating(t *testing.T) {
	var p Pool
	atomic.StoreUint64(&p.calibrating, 1)
	atomic.StoreUint64(&p.calls[0], 7)

	p.calibrate()

	if got := atomic.LoadUint64(&p.calls[0]); got != 7 {
		t.Fatalf("calls[0] = %d, want 7 when calibration is skipped", got)
	}
	if got := atomic.LoadUint64(&p.defaultSize); got != 0 {
		t.Fatalf("defaultSize = %d, want 0 when calibration is skipped", got)
	}
}
