package integration_test

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/DevNewbie1826/http-over-polling/hop"
	"github.com/gin-gonic/gin"
	"github.com/go-chi/chi/v5"
	"github.com/labstack/echo/v4"
)

func TestChiCompatibility(t *testing.T) {
	addr := nextLoopbackAddr(t)
	r := chi.NewRouter()
	r.Get("/chi", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Framework", "chi")
		_, _ = w.Write([]byte("chi ok"))
	})
	assertServerResponds(t, addr, r, "/chi", "chi ok", "chi")
}

func TestGinCompatibility(t *testing.T) {
	gin.SetMode(gin.ReleaseMode)
	addr := nextLoopbackAddr(t)
	r := gin.New()
	r.GET("/gin", func(c *gin.Context) {
		c.Header("X-Framework", "gin")
		c.String(http.StatusOK, "gin ok")
	})
	assertServerResponds(t, addr, r, "/gin", "gin ok", "gin")
}

func TestEchoCompatibility(t *testing.T) {
	addr := nextLoopbackAddr(t)
	e := echo.New()
	e.GET("/echo", func(c echo.Context) error {
		c.Response().Header().Set("X-Framework", "echo")
		return c.String(http.StatusOK, "echo ok")
	})
	assertServerResponds(t, addr, e, "/echo", "echo ok", "echo")
}

func assertServerResponds(t *testing.T, addr string, handler http.Handler, path, wantBody, wantHeader string) {
	t.Helper()
	errCh := make(chan error, 1)
	go func() {
		errCh <- hop.ListenAndServe(addr, handler)
	}()

	url := fmt.Sprintf("http://127.0.0.1%s%s", addr, path)
	deadline := time.Now().Add(5 * time.Second)
	for {
		resp, err := http.Get(url)
		if err == nil {
			body, readErr := io.ReadAll(resp.Body)
			_ = resp.Body.Close()
			if readErr != nil {
				t.Fatalf("ReadAll() error = %v", readErr)
			}
			if got := string(body); got != wantBody {
				t.Fatalf("body = %q, want %q", got, wantBody)
			}
			if got := resp.Header.Get("X-Framework"); got != wantHeader {
				t.Fatalf("X-Framework = %q, want %q", got, wantHeader)
			}
			return
		}
		if time.Now().After(deadline) {
			select {
			case serveErr := <-errCh:
				t.Fatalf("server did not become ready; ListenAndServe returned %v", serveErr)
			default:
			}
			t.Fatalf("server did not become ready before deadline: %v", err)
		}
		time.Sleep(25 * time.Millisecond)
	}
}

func nextLoopbackAddr(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen() error = %v", err)
	}
	defer ln.Close()
	return fmt.Sprintf(":%d", ln.Addr().(*net.TCPAddr).Port)
}
