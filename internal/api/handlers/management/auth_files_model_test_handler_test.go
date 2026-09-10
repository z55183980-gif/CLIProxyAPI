package management

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/registry"
	coreauth "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/auth"
	"github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/executor"
	"github.com/router-for-me/CLIProxyAPI/v7/sdk/translator"
	"github.com/tidwall/gjson"
)

type modelProbeExecutor struct {
	coreauth.ProviderExecutor
	calls   []string
	model   string
	payload []byte
	err     error
}

func (e *modelProbeExecutor) Identifier() string { return "model-probe-test" }
func (e *modelProbeExecutor) Execute(ctx context.Context, auth *coreauth.Auth, req executor.Request, opts executor.Options) (executor.Response, error) {
	e.calls = append(e.calls, auth.ID)
	e.model = req.Model
	if opts.SourceFormat != translator.FormatOpenAI || gjson.GetBytes(req.Payload, "messages.0.content").String() != "Reply with OK." {
		return executor.Response{}, &coreauth.Error{Message: "invalid generation request", HTTPStatus: 400}
	}
	if ctx.Err() != nil {
		return executor.Response{}, ctx.Err()
	}
	return executor.Response{Payload: e.payload}, e.err
}

func TestAuthFileModel(t *testing.T) {
	for _, scenario := range []struct {
		name     string
		body     string
		disabled bool
		payload  string
		err      error
		status   int
		success  bool
		called   bool
	}{
		{name: "selected credential and alias", success: true, called: true, status: 200},
		{name: "upstream unauthorized stays a test failure", err: &coreauth.Error{HTTPStatus: 401, Message: "private-token-must-not-leak"}, called: true, status: 200},
		{name: "invalid completion", payload: `{}`, called: true, status: 200},
		{name: "missing model", body: `{"name":"shared.json","auth_index":"idx-b"}`, status: 400},
		{name: "unknown model", body: `{"name":"shared.json","auth_index":"idx-b","model":"unknown"}`, status: 400},
		{name: "wrong index", body: `{"name":"shared.json","auth_index":"idx-missing","model":"probe-alias"}`, status: 404},
		{name: "disabled credential", disabled: true, status: 409},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			manager := coreauth.NewManager(nil, nil, nil)
			manager.SetRetryConfig(0, 0, 0)
			probe := &modelProbeExecutor{payload: []byte(`{"choices":[{"message":{"content":"OK"},"finish_reason":"stop"}]}`), err: scenario.err}
			if scenario.payload != "" {
				probe.payload = []byte(scenario.payload)
			}
			manager.RegisterExecutor(probe)
			manager.SetOAuthModelAlias(map[string][]config.OAuthModelAlias{probe.Identifier(): {{Name: "probe-upstream", Alias: "probe-alias"}}})
			for _, id := range []string{"a", "b"} {
				authID := "model-probe-" + id
				_, errRegister := manager.Register(context.Background(), &coreauth.Auth{
					ID: authID, FileName: "shared.json", Index: "idx-" + id, Provider: probe.Identifier(),
					Status: coreauth.StatusActive, Disabled: id == "b" && scenario.disabled,
				})
				if errRegister != nil {
					t.Fatal(errRegister)
				}
				registry.GetGlobalRegistry().RegisterClient(authID, probe.Identifier(), []*registry.ModelInfo{{ID: "probe-alias"}})
				t.Cleanup(func() { registry.GetGlobalRegistry().UnregisterClient(authID) })
			}
			body := scenario.body
			if body == "" {
				body = `{"name":"shared.json","auth_index":"idx-b","model":"probe-alias"}`
			}
			rec := httptest.NewRecorder()
			ctx, _ := gin.CreateTestContext(rec)
			ctx.Request = httptest.NewRequest(http.MethodPost, "/auth-files/test-model", strings.NewReader(body))
			(&Handler{authManager: manager}).TestAuthFileModel(ctx)
			if rec.Code != scenario.status {
				t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
			}
			var result struct {
				Success    bool   `json:"success"`
				Model      string `json:"model"`
				StatusCode int    `json:"status_code"`
			}
			if errDecode := json.Unmarshal(rec.Body.Bytes(), &result); errDecode != nil {
				t.Fatal(errDecode)
			}
			if result.Success != scenario.success {
				t.Fatalf("result: %s", rec.Body.String())
			}
			if scenario.called {
				if len(probe.calls) != 1 || probe.calls[0] != "model-probe-b" || probe.model != "probe-upstream" || result.Model != "probe-alias" {
					t.Fatalf("calls=%v upstream=%s result=%s", probe.calls, probe.model, rec.Body.String())
				}
			} else if len(probe.calls) != 0 {
				t.Fatalf("unexpected upstream calls: %v", probe.calls)
			}
			if scenario.err != nil && result.StatusCode != 401 {
				t.Fatalf("missing upstream status: %s", rec.Body.String())
			}
			if strings.Contains(rec.Body.String(), "private-token") {
				t.Fatal("error leaked credential")
			}
		})
	}
}
