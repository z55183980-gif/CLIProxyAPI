package executor

import (
	"context"
	"net/http"
	"net/http/httptest"

	"github.com/gin-gonic/gin"
	// Executors use the same built-in translator registrations as cmd/server.
	_ "github.com/router-for-me/CLIProxyAPI/v7/internal/translator"
)

// Provider-neutral test helpers.
//
// roundTripperFunc was originally declared in
// antigravity_executor_credits_test.go, which was removed with the Antigravity
// upstream. The adapter is provider-neutral and claude_executor_auth_test.go
// depends on it, so it lives here now.

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func contextWithGinHeaders(headers map[string]string) context.Context {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ginCtx, _ := gin.CreateTestContext(recorder)
	ginCtx.Request = httptest.NewRequest(http.MethodPost, "/", nil)
	ginCtx.Request.Header = make(http.Header, len(headers))
	for key, value := range headers {
		ginCtx.Request.Header.Set(key, value)
	}
	return context.WithValue(context.Background(), "gin", ginCtx)
}
