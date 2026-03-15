package hop

import (
	"io"
	"sync"
	"testing"
)

var compositeBufferBenchmarkChunks = [][]byte{
	[]byte("chunk-0000-abcdefgh"),
	[]byte("chunk-0001-ijklmnop"),
	[]byte("chunk-0002-qrstuvwx"),
	[]byte("chunk-0003-yzabcdef"),
}

var compositeBufferPool = sync.Pool{
	New: func() any {
		return &CompositeBuffer{}
	},
}

func TestCompositeBufferWriteCloneOwnsBytes(t *testing.T) {
	var buf CompositeBuffer
	src := []byte("hello")
	if _, err := buf.WriteClone(src); err != nil {
		t.Fatalf("WriteClone() error = %v", err)
	}
	src[0] = 'j'
	got, err := io.ReadAll(&buf)
	if err != nil {
		t.Fatalf("ReadAll() error = %v", err)
	}
	if string(got) != "hello" {
		t.Fatalf("body = %q, want %q", got, "hello")
	}
}

func TestCompositeBufferWriteAliasSharesBytes(t *testing.T) {
	var buf CompositeBuffer
	src := []byte("hello")
	if _, err := buf.WriteAlias(src); err != nil {
		t.Fatalf("WriteAlias() error = %v", err)
	}
	src[0] = 'j'
	got, err := io.ReadAll(&buf)
	if err != nil {
		t.Fatalf("ReadAll() error = %v", err)
	}
	if string(got) != "jello" {
		t.Fatalf("body = %q, want %q", got, "jello")
	}
}

func TestCompositeBufferWriteClonesAndAppends(t *testing.T) {
	var buf CompositeBuffer

	first := []byte("hello")
	n, err := buf.Write(first)
	if err != nil {
		t.Fatalf("Write(first) error = %v", err)
	}
	if n != len(first) {
		t.Fatalf("Write(first) n = %d, want %d", n, len(first))
	}
	first[0] = 'j'

	second := []byte(" world")
	n, err = buf.Write(second)
	if err != nil {
		t.Fatalf("Write(second) error = %v", err)
	}
	if n != len(second) {
		t.Fatalf("Write(second) n = %d, want %d", n, len(second))
	}
	second[1] = 'W'

	got, err := io.ReadAll(&buf)
	if err != nil {
		t.Fatalf("ReadAll() error = %v", err)
	}
	if string(got) != "hello world" {
		t.Fatalf("body = %q, want %q", got, "hello world")
	}
}

func BenchmarkCompositeBufferWriteCloneReuse(b *testing.B) {
	var buf CompositeBuffer
	total := 0
	for _, chunk := range compositeBufferBenchmarkChunks {
		total += len(chunk)
	}
	b.ReportAllocs()
	b.SetBytes(int64(total))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		buf.Reset()
		for _, chunk := range compositeBufferBenchmarkChunks {
			if _, err := buf.WriteClone(chunk); err != nil {
				b.Fatalf("WriteClone() error = %v", err)
			}
		}
	}
}

func BenchmarkCompositeBufferWriteClonePooled(b *testing.B) {
	total := 0
	for _, chunk := range compositeBufferBenchmarkChunks {
		total += len(chunk)
	}
	b.ReportAllocs()
	b.SetBytes(int64(total))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		buf := compositeBufferPool.Get().(*CompositeBuffer)
		buf.Reset()
		for _, chunk := range compositeBufferBenchmarkChunks {
			if _, err := buf.WriteClone(chunk); err != nil {
				b.Fatalf("WriteClone() error = %v", err)
			}
		}
		buf.Reset()
		compositeBufferPool.Put(buf)
	}
}
