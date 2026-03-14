package main

import (
	"io"
	"net/http"
	"os"

	"github.com/DevNewbie1826/http-over-polling/examples/cmd/benchparity"
)

func main() {
	addr := os.Getenv("ADDR")
	if addr == "" {
		addr = ":8080"
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.Body != nil {
			_, _ = io.Copy(io.Discard, r.Body)
			_ = r.Body.Close()
		}
		_, _ = w.Write(benchparity.HTTPResponseBodyBytes)
	})

	server := &http.Server{Addr: addr, Handler: mux}
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		panic(err)
	}
}
