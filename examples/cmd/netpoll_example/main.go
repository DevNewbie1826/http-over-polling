package main

import (
	"io"
	"net/http"
	"os"

	"github.com/DevNewbie1826/http-over-polling/examples/cmd/benchparity"
	"github.com/DevNewbie1826/http-over-polling/hop"
)

func main() {
	addr := os.Getenv("ADDR")
	if addr == "" {
		addr = ":8080"
	}

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Body != nil {
			_, _ = io.Copy(io.Discard, r.Body)
			_ = r.Body.Close()
		}
		_, _ = w.Write(benchparity.HTTPResponseBodyBytes)
	})
	if err := hop.ListenAndServe(addr, handler); err != nil {
		panic(err)
	}
}
