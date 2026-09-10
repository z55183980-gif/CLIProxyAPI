package management

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/usagehistory"
)

func TestUsageHistoryQueriesAndBoundedCleanup(t *testing.T) {
	s, err := usagehistory.Open(filepath.Join(t.TempDir(), "usage.db"))
	if err != nil {
		t.Fatal(err)
	}
	usagehistory.SetActive(s)
	defer s.Close()
	if err = s.Append(context.Background(), usagehistory.Record{Timestamp: "2026-09-09T00:00:00Z", Model: "fixture"}); err != nil {
		t.Fatal(err)
	}
	h := &Handler{}
	call := func(method, url, body string, handler gin.HandlerFunc) int {
		t.Helper()
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(method, url, strings.NewReader(body))
		c.Request.Header.Set("Content-Type", "application/json")
		handler(c)
		return w.Code
	}
	if got := call(http.MethodGet, "/usage-history?sort=invalid", "", h.GetUsageHistory); got != 400 {
		t.Fatalf("invalid sort=%d", got)
	}
	if got := call(http.MethodGet, "/usage-history?page_size=25", "", h.GetUsageHistory); got != 200 {
		t.Fatalf("query=%d", got)
	}
	for _, body := range []string{`{"confirm":false}`, `{"confirm":true,"filter":{}}`} {
		if got := call(http.MethodDelete, "/usage-history", body, h.DeleteUsageHistory); got != 400 {
			t.Fatalf("unsafe cleanup=%d", got)
		}
	}
	if got := call(http.MethodDelete, "/usage-history", `{"confirm":true,"filter":{"start":"2026-09-09T00:00:00Z","end":"2026-09-10T00:00:00Z"}}`, h.DeleteUsageHistory); got != 200 {
		t.Fatalf("cleanup=%d", got)
	}
}

func TestAccountTodayStatsBatchAPI(t *testing.T) {
	s, err := usagehistory.Open(filepath.Join(t.TempDir(), "usage.db"))
	if err != nil {
		t.Fatal(err)
	}
	usagehistory.SetActive(s)
	t.Cleanup(func() {
		if err := s.Close(); err != nil {
			t.Error(err)
		}
	})
	if err = s.Append(context.Background(), usagehistory.Record{Timestamp: "2026-09-09T00:00:00+08:00", AccountID: "account_with_underscore", Model: "gpt-5.4", InputTokens: 100, TotalTokens: 100, AccountingQuality: "complete"}); err != nil {
		t.Fatal(err)
	}
	h := &Handler{}
	for _, tc := range []struct {
		body string
		code int
	}{
		{`{"account_ids":["account_with_underscore","empty"],"start":"2026-09-09T00:00:00+08:00","end":"2026-09-10T00:00:00+08:00"}`, 200},
		{`{"account_ids":["a"]}`, 400},
		{`{"account_ids":[" "],"start":"2026-09-09T00:00:00Z","end":"2026-09-10T00:00:00Z"}`, 400},
		{`{"account_ids":["a"],"start":"2026-09-10T00:00:00Z","end":"2026-09-09T00:00:00Z"}`, 400},
		{`{`, 400},
	} {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodPost, "/usage-history/accounts/today", strings.NewReader(tc.body))
		c.Request.Header.Set("Content-Type", "application/json")
		h.PostAccountTodayStats(c)
		if w.Code != tc.code {
			t.Fatalf("response %d: %s", w.Code, w.Body.String())
		}
		if w.Code == 200 {
			var body struct {
				Stats map[string]usagehistory.AccountStats `json:"stats"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
				t.Fatal(err)
			}
			if len(body.Stats) != 2 || body.Stats["account_with_underscore"].Requests != 1 || body.Stats["account_with_underscore"].Tokens != 100 || body.Stats["account_with_underscore"].Cost != 0.00025 || body.Stats["empty"] != (usagehistory.AccountStats{}) {
				t.Fatalf("unexpected stats: %+v", body)
			}
		}
	}
}
