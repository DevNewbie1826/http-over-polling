package main

import (
	"os"
	"path/filepath"
	"runtime"

	"github.com/DevNewbie1826/http-over-polling/examples/internal/wsapp"
)

func main() {
	addr := os.Getenv("ADDR")
	if addr == "" {
		addr = ":8080"
	}

	_, thisFile, _, _ := runtime.Caller(0)
	filePath := filepath.Clean(thisFile)
	if err := wsapp.NewServer(wsapp.ServerKindHop, addr, filePath).ListenAndServe(); err != nil {
		panic(err)
	}
}
