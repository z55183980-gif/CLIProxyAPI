package api

import (
	"context"
	"net/http"
	"testing"
	"time"
)

func TestServerConcurrentStartupShutdown(t *testing.T) {
	for i := 0; i < 32; i++ {
		server := &Server{server: &http.Server{Addr: "127.0.0.1:0", Handler: http.NotFoundHandler()}}
		done := make(chan error, 1)
		go func() { done <- server.Start() }()
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		err := server.Stop(ctx)
		cancel()
		if err != nil {
			t.Fatal(err)
		}
		select {
		case err := <-done:
			if err != nil {
				t.Fatal(err)
			}
		case <-time.After(time.Second):
			t.Fatal("Start kept running after Stop")
		}
		if err := server.Start(); err != nil {
			t.Fatal(err)
		}
	}
}
