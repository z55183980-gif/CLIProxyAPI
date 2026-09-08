package usage

import "testing"

func TestBillingTotalsAggregatesByAccountAndDeduplicates(t *testing.T) {
	a := NewBillingTotals()
	c := Charge{EventID: "e1", APIKey: "key", Record: Record{AuthID: "acct"}, Quote: TokenQuote{InputTokens: 2, OutputTokens: 3, TotalMicros: 7, TotalUSD: 0.000007}}
	a.Add(c)
	a.Add(c)
	a.Add(Charge{EventID: "e2", APIKey: "key", Record: Record{AuthID: "acct"}, Quote: TokenQuote{InputTokens: 1, TotalMicros: 2, TotalUSD: 0.000002}})
	rows := a.Snapshot()
	if len(rows) != 2 {
		t.Fatalf("rows=%d", len(rows))
	}
	if rows[0].Account != "acct" {
		t.Fatalf("account=%q", rows[0].Account)
	}
	if rows[0].Requests != 2 || rows[0].TotalTokens != 6 || rows[0].TotalMicros != 9 {
		t.Fatalf("unexpected row: %+v", rows[0])
	}
	total := rows[1]
	if total.Account != "total" || total.TotalMicros != 9 {
		t.Fatalf("unexpected total: %+v", total)
	}
}
