package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"runtime"
	"testing"
	"time"
)

type hopBenchConfig struct {
	name        string
	pollerNum   int
	bufferSize  int
	readTimeout time.Duration
}

func BenchmarkMatrix_HopHTTP(b *testing.B) {
	mux := http.NewServeMux()
	mux.HandleFunc("/", benchmarkRootHandler)

	gomax := runtime.GOMAXPROCS(0)
	half := gomax / 2
	if half < 1 {
		half = 1
	}
	quarter := gomax / 4
	if quarter < 1 {
		quarter = 1
	}

	configs := []hopBenchConfig{
		{name: "p1_b512_rt0", pollerNum: 1, bufferSize: 512, readTimeout: 0},
		{name: "pQuarter_b1024_rt0", pollerNum: quarter, bufferSize: 1024, readTimeout: 0},
		{name: "pHalf_b4096_rt0", pollerNum: half, bufferSize: 4096, readTimeout: 0},
		{name: "pQuarter_b1024_rt1s", pollerNum: quarter, bufferSize: 1024, readTimeout: time.Second},
	}

	for _, cfg := range configs {
		cfg := cfg
		b.Run(cfg.name, func(b *testing.B) {
			srv := startHopHTTPBenchmarkServerWithConfig(b, mux, cfg.readTimeout, cfg.pollerNum, cfg.bufferSize)
			defer srv.shutdown(context.Background())

			client := getBenchmarkClient()
			url := "http://" + srv.addr + "/"

			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				resp, err := client.Get(url)
				if err != nil {
					b.Fatalf("Get() error: %v", err)
				}
				if _, err := io.Copy(io.Discard, resp.Body); err != nil {
					resp.Body.Close()
					b.Fatalf("Copy() error: %v", err)
				}
				resp.Body.Close()
			}
			b.StopTimer()
			b.Logf("poller=%d buffer=%d readTimeout=%s", cfg.pollerNum, cfg.bufferSize, cfg.readTimeout)
		})
	}
}

func BenchmarkMatrix_HopHTTP_SmokeReport(b *testing.B) {
	gomax := runtime.GOMAXPROCS(0)
	quarter := gomax / 4
	if quarter < 1 {
		quarter = 1
	}
	b.ReportMetric(float64(quarter), "poller(default-quarter)")
	b.ReportMetric(float64(gomax), "gomaxprocs")
	fmt.Fprintf(io.Discard, "")
}
