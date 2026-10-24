package pipeline

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/openaccountants/firefly-iii-ai-categorize/internal/cache"
	"github.com/openaccountants/firefly-iii-ai-categorize/internal/classifier"
	"github.com/openaccountants/firefly-iii-ai-categorize/internal/firefly"
	"github.com/openaccountants/firefly-iii-ai-categorize/internal/job"
)

func TestResolveMatchedAccountPrefersID(t *testing.T) {
	accounts := []firefly.Account{{ID: "1", Name: "AMAZON"}, {ID: "2", Name: "Amazon"}}

	got := resolveMatchedAccount(accounts, &classifier.DestinationResult{ID: "2", Name: "Amazon", Action: "MATCH"})
	if got.ID != "2" {
		t.Errorf("by ID: got %+v, want account 2", got)
	}

	got = resolveMatchedAccount(accounts, &classifier.DestinationResult{Name: "amazon", Action: "MATCH"})
	if got.ID != "1" {
		t.Errorf("by name fallback: got %+v, want first case-insensitive match", got)
	}

	got = resolveMatchedAccount(accounts, &classifier.DestinationResult{ID: "99", Name: "Amazon", Action: "MATCH"})
	if got.ID != "1" {
		t.Errorf("unknown ID must fall back to name: got %+v", got)
	}

	if got := resolveMatchedAccount(accounts, &classifier.DestinationResult{Name: "Lidl"}); got.ID != "" {
		t.Errorf("no match: got %+v", got)
	}
}

func TestHistoryOverrideReason(t *testing.T) {
	if r := historyOverrideReason(3, classifier.SourceJev); !strings.Contains(r, "Jev was consulted for destination") || strings.Contains(r, "AI was") {
		t.Errorf("jev source: %q", r)
	}
	for _, src := range []string{classifier.SourceJevLLM, classifier.SourceLLM, ""} {
		if r := historyOverrideReason(3, src); !strings.Contains(r, "AI was consulted for destination") {
			t.Errorf("source %q: %q", src, r)
		}
	}
	if r := historyOverrideReason(3, ""); !strings.HasPrefix(r, "Auto-matched from 3 previous transactions.") {
		t.Errorf("prefix: %q", r)
	}
}

// recordingClassifier captures the request it receives and returns a fixed result.
type recordingClassifier struct {
	t                   *testing.T
	wantDestinationName string
	result              classifier.Result
}

func (fc *recordingClassifier) Classify(ctx context.Context, req classifier.Request) (classifier.Result, error) {
	if req.DestinationName != fc.wantDestinationName {
		fc.t.Errorf("Classify DestinationName = %q, want %q", req.DestinationName, fc.wantDestinationName)
	}
	return fc.result, nil
}

// newTestFireflyServer returns a minimal Firefly III mock that satisfies the
// endpoints used during a pipeline run.
func newTestFireflyServer(t *testing.T) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/api/v1/categories":
			json.NewEncoder(w).Encode(map[string]interface{}{
				"data": []map[string]interface{}{{
					"id":         "cat-1",
					"attributes": map[string]interface{}{"name": "Groceries"},
				}},
				"meta": map[string]interface{}{"pagination": map[string]interface{}{"total_pages": 1, "total": 1}},
			})
		case strings.HasPrefix(r.URL.Path, "/api/v1/accounts"):
			json.NewEncoder(w).Encode(map[string]interface{}{
				"data": []map[string]interface{}{{
					"id":         "acc-1",
					"attributes": map[string]interface{}{"name": "Supermarket"},
				}},
				"meta": map[string]interface{}{"pagination": map[string]interface{}{"total_pages": 1, "total": 1}},
			})
		case r.URL.Path == "/api/v1/transactions":
			json.NewEncoder(w).Encode(map[string]interface{}{
				"data": []map[string]interface{}{},
				"meta": map[string]interface{}{"pagination": map[string]interface{}{"total_pages": 0, "total": 0}},
			})
		case strings.HasPrefix(r.URL.Path, "/api/v1/transactions/") && r.Method == http.MethodPut:
			w.WriteHeader(http.StatusOK)
		default:
			t.Fatalf("unexpected %s %s", r.Method, r.URL.Path)
		}
	}))
}

func TestRunWithOptionsClearsUnknownDestination(t *testing.T) {
	server := newTestFireflyServer(t)
	defer server.Close()

	fc := firefly.New(server.URL, "token", "ai")
	reg := job.NewRegistry()
	ca := cache.New(fc, 0, 1)

	amount := 12.34
	j := reg.Create("txn-1", "", "(unknown destination)", "Grocery store", &amount)
	txn := firefly.Transaction{
		ID: "txn-1",
		Splits: []firefly.Split{{
			JournalID:       "j-1",
			Description:     "Grocery store",
			DestinationName: "(unknown destination)",
			Amount:          "12.34",
		}},
	}

	cl := &recordingClassifier{
		t:                   t,
		wantDestinationName: "",
		result: classifier.Result{
			Outcome:  classifier.Classified,
			Category: "Groceries",
			Destination: &classifier.DestinationResult{
				Name:       "Supermarket",
				Action:     "MATCH",
				Confidence: "CLASSIFIED",
			},
		},
	}
	pipe := New(fc, cl, ca, reg, 1, true, false)

	if err := pipe.RunWithOptions(context.Background(), j, txn, RunOptions{ClassifyCategory: true, MatchDestination: true}); err != nil {
		t.Fatalf("RunWithOptions: %v", err)
	}

	if j.DestinationName != "" {
		t.Errorf("job DestinationName = %q, want empty", j.DestinationName)
	}
	if txn.Splits[0].DestinationName != "" {
		t.Errorf("split DestinationName = %q, want empty", txn.Splits[0].DestinationName)
	}
	stored, _ := reg.Get(j.ID)
	if stored.DestinationName != "" {
		t.Errorf("registry DestinationName = %q, want empty", stored.DestinationName)
	}
}

func TestRunWithOptionsKeepsUnknownDestinationWhenDisabled(t *testing.T) {
	server := newTestFireflyServer(t)
	defer server.Close()

	fc := firefly.New(server.URL, "token", "ai")
	reg := job.NewRegistry()
	ca := cache.New(fc, 0, 1)

	amount := 12.34
	j := reg.Create("txn-2", "", "(unknown destination)", "Grocery store", &amount)
	txn := firefly.Transaction{
		ID: "txn-2",
		Splits: []firefly.Split{{
			JournalID:       "j-2",
			Description:     "Grocery store",
			DestinationName: "(unknown destination)",
			Amount:          "12.34",
		}},
	}

	cl := &recordingClassifier{
		t:                   t,
		wantDestinationName: "(unknown destination)",
		result: classifier.Result{
			Outcome:  classifier.Classified,
			Category: "Groceries",
		},
	}
	pipe := New(fc, cl, ca, reg, 1, false, false)

	if err := pipe.RunWithOptions(context.Background(), j, txn, RunOptions{ClassifyCategory: true, MatchDestination: false}); err != nil {
		t.Fatalf("RunWithOptions: %v", err)
	}

	if j.DestinationName != "(unknown destination)" {
		t.Errorf("job DestinationName = %q, want unchanged", j.DestinationName)
	}
	if txn.Splits[0].DestinationName != "(unknown destination)" {
		t.Errorf("split DestinationName = %q, want unchanged", txn.Splits[0].DestinationName)
	}
}

func TestRunWithOptionsClearsCaseInsensitiveUnknownDestination(t *testing.T) {
	server := newTestFireflyServer(t)
	defer server.Close()

	fc := firefly.New(server.URL, "token", "ai")
	reg := job.NewRegistry()
	ca := cache.New(fc, 0, 1)

	amount := 12.34
	j := reg.Create("txn-3", "", "  Unknown Destination  ", "Grocery store", &amount)
	txn := firefly.Transaction{
		ID: "txn-3",
		Splits: []firefly.Split{{
			JournalID:       "j-3",
			Description:     "Grocery store",
			DestinationName: "unknown destination",
			Amount:          "12.34",
		}},
	}

	cl := &recordingClassifier{
		t:                   t,
		wantDestinationName: "",
		result: classifier.Result{
			Outcome:  classifier.Classified,
			Category: "Groceries",
			Destination: &classifier.DestinationResult{
				Name:       "Supermarket",
				Action:     "MATCH",
				Confidence: "CLASSIFIED",
			},
		},
	}
	pipe := New(fc, cl, ca, reg, 1, true, false)

	if err := pipe.RunWithOptions(context.Background(), j, txn, RunOptions{ClassifyCategory: true, MatchDestination: true}); err != nil {
		t.Fatalf("RunWithOptions: %v", err)
	}

	if j.DestinationName != "" {
		t.Errorf("job DestinationName = %q, want empty", j.DestinationName)
	}
	if txn.Splits[0].DestinationName != "" {
		t.Errorf("split DestinationName = %q, want empty", txn.Splits[0].DestinationName)
	}
}

// assertingClassifier checks the request it receives and records whether it was called.
type assertingClassifier struct {
	t                   *testing.T
	wantDestinationName string
	wantDestinationOnly bool
	called              bool
	result              classifier.Result
}

func (ac *assertingClassifier) Classify(ctx context.Context, req classifier.Request) (classifier.Result, error) {
	ac.called = true
	if req.DestinationName != ac.wantDestinationName {
		ac.t.Errorf("Classify DestinationName = %q, want %q", req.DestinationName, ac.wantDestinationName)
	}
	if req.DestinationOnly != ac.wantDestinationOnly {
		ac.t.Errorf("Classify DestinationOnly = %v, want %v", req.DestinationOnly, ac.wantDestinationOnly)
	}
	return ac.result, nil
}

// encodeTransactions returns the JSON shape expected by the Firefly III
// transactions list endpoint.
func encodeTransactions(txns []firefly.Transaction) []map[string]interface{} {
	data := make([]map[string]interface{}, len(txns))
	for i, txn := range txns {
		splits := make([]map[string]interface{}, len(txn.Splits))
		for j, s := range txn.Splits {
			splits[j] = map[string]interface{}{
				"transaction_journal_id": s.JournalID,
				"type":                   s.Type,
				"description":            s.Description,
				"destination_name":       s.DestinationName,
				"destination_id":         s.DestinationID,
				"amount":                 s.Amount,
				"category_name":          s.CategoryName,
				"category_id":            s.CategoryID,
			}
		}
		data[i] = map[string]interface{}{
			"id": txn.ID,
			"attributes": map[string]interface{}{
				"group_title":  txn.GroupTitle,
				"transactions": splits,
			},
		}
	}
	return data
}

// newMockFireflyServer returns a server configured with the given accounts,
// categories, budgets and past transactions, and records the body of any transaction PUT
// in lastBody.
func newMockFireflyServer(t *testing.T, categories []firefly.Category, accounts []firefly.Account, budgets []firefly.Budget, txns []firefly.Transaction, lastBody *[]byte) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/api/v1/categories":
			catData := make([]map[string]interface{}, len(categories))
			for i, c := range categories {
				catData[i] = map[string]interface{}{"id": c.ID, "attributes": map[string]interface{}{"name": c.Name}}
			}
			json.NewEncoder(w).Encode(map[string]interface{}{
				"data": catData,
				"meta": map[string]interface{}{"pagination": map[string]interface{}{"total_pages": 1, "total": len(categories)}},
			})
		case r.URL.Path == "/api/v1/budgets":
			budgetData := make([]map[string]interface{}, len(budgets))
			for i, b := range budgets {
				budgetData[i] = map[string]interface{}{"id": b.ID, "attributes": map[string]interface{}{"name": b.Name, "active": true}}
			}
			json.NewEncoder(w).Encode(map[string]interface{}{
				"data": budgetData,
				"meta": map[string]interface{}{"pagination": map[string]interface{}{"total_pages": 1, "total": len(budgets)}},
			})
		case strings.HasPrefix(r.URL.Path, "/api/v1/accounts"):
			acctData := make([]map[string]interface{}, len(accounts))
			for i, a := range accounts {
				acctData[i] = map[string]interface{}{"id": a.ID, "attributes": map[string]interface{}{"name": a.Name}}
			}
			json.NewEncoder(w).Encode(map[string]interface{}{
				"data": acctData,
				"meta": map[string]interface{}{"pagination": map[string]interface{}{"total_pages": 1, "total": len(accounts)}},
			})
		case r.URL.Path == "/api/v1/transactions":
			json.NewEncoder(w).Encode(map[string]interface{}{
				"data": encodeTransactions(txns),
				"meta": map[string]interface{}{"pagination": map[string]interface{}{"total_pages": 1, "total": len(txns)}},
			})
		case strings.HasPrefix(r.URL.Path, "/api/v1/transactions/") && r.Method == http.MethodPut:
			body, _ := io.ReadAll(r.Body)
			*lastBody = body
			w.WriteHeader(http.StatusOK)
		default:
			t.Fatalf("unexpected %s %s", r.Method, r.URL.Path)
		}
	}))
}

func TestTryDestinationHistoryMatchIgnoresUnknown(t *testing.T) {
	hist := []classifier.HistoricalEntry{
		{DestinationAccountID: "29", DestinationName: "unknown", Amount: 10},
		{DestinationAccountID: "29", DestinationName: "Unknown", Amount: 10},
		{DestinationAccountID: "29", DestinationName: "", Amount: 10},
		{DestinationAccountID: "7", DestinationName: "Shell", Amount: 10},
		{DestinationAccountID: "7", DestinationName: "Shell", Amount: 10},
	}

	id, count := tryDestinationHistoryMatch(hist, floatPtr(10))
	if id != "7" || count != 2 {
		t.Errorf("mixed history: got id=%q count=%d, want id=7 count=2", id, count)
	}

	// Entries with unknown destination names or empty IDs must never accumulate votes.
	unknownOnly := []classifier.HistoricalEntry{
		{DestinationAccountID: "29", DestinationName: "unknown", Amount: 10},
		{DestinationAccountID: "29", DestinationName: "Unknown", Amount: 10},
		{DestinationAccountID: "29", DestinationName: "(unknown destination)", Amount: 10},
	}
	id, count = tryDestinationHistoryMatch(unknownOnly, floatPtr(10))
	if id != "" || count != 0 {
		t.Errorf("unknown-only history: got id=%q count=%d, want empty", id, count)
	}
}

func TestRunWithOptionsIgnoresUnknownDestinationHistory(t *testing.T) {
	var lastBody []byte
	categories := []firefly.Category{{ID: "cat-1", Name: "Supermercado"}}
	accounts := []firefly.Account{
		{ID: "29", Name: "unknown"},
		{ID: "acc-real", Name: "Real Store"},
	}

	desc := "Weekly shopping"
	histTxns := []firefly.Transaction{
		{ID: "old-1", Splits: []firefly.Split{{
			JournalID: "j-old-1", Type: "withdrawal", Description: desc,
			DestinationName: "unknown", DestinationID: "29", Amount: "42.50",
			CategoryName: "Supermercado", CategoryID: "cat-1",
		}}},
		{ID: "old-2", Splits: []firefly.Split{{
			JournalID: "j-old-2", Type: "withdrawal", Description: desc,
			DestinationName: "unknown", DestinationID: "29", Amount: "42.50",
			CategoryName: "Supermercado", CategoryID: "cat-1",
		}}},
		{ID: "old-3", Splits: []firefly.Split{{
			JournalID: "j-old-3", Type: "withdrawal", Description: desc,
			DestinationName: "unknown", DestinationID: "29", Amount: "42.50",
			CategoryName: "Supermercado", CategoryID: "cat-1",
		}}},
	}

	server := newMockFireflyServer(t, categories, accounts, nil, histTxns, &lastBody)
	defer server.Close()

	fc := firefly.New(server.URL, "token", "ai")
	reg := job.NewRegistry()
	ca := cache.New(fc, 0, 1)

	amount := 42.50
	j := reg.Create("txn-new", "", "unknown", desc, &amount)
	txn := firefly.Transaction{
		ID: "txn-new",
		Splits: []firefly.Split{{
			JournalID:       "j-new",
			Description:     desc,
			DestinationName: "unknown",
			Amount:          "42.50",
		}},
	}

	cl := &assertingClassifier{
		t:                   t,
		wantDestinationName: "",
		wantDestinationOnly: true,
		result: classifier.Result{
			Outcome:  classifier.Classified,
			Category: "Ignored",
			Destination: &classifier.DestinationResult{
				Name:       "Real Store",
				Action:     "MATCH",
				ID:         "acc-real",
				Confidence: "CLASSIFIED",
			},
		},
	}
	pipe := New(fc, cl, ca, reg, 1, true, false)

	if err := pipe.RunWithOptions(context.Background(), j, txn, RunOptions{ClassifyCategory: true, MatchDestination: true}); err != nil {
		t.Fatalf("RunWithOptions: %v", err)
	}

	if !cl.called {
		t.Fatalf("classifier was not called; transaction should not have been auto-matched")
	}

	if j.DestinationName != "" {
		t.Errorf("job DestinationName = %q, want empty", j.DestinationName)
	}

	stored, _ := reg.Get(j.ID)
	if stored.DestinationName != "" {
		t.Errorf("registry DestinationName = %q, want empty", stored.DestinationName)
	}
	if stored.DestinationAccount != "Real Store" {
		t.Errorf("registry DestinationAccount = %q, want Real Store", stored.DestinationAccount)
	}
	if stored.DestinationAction != "MATCH" {
		t.Errorf("registry DestinationAction = %q, want MATCH", stored.DestinationAction)
	}

	var body struct {
		Transactions []struct {
			CategoryID    string `json:"category_id"`
			DestinationID string `json:"destination_id"`
		} `json:"transactions"`
	}
	if err := json.Unmarshal(lastBody, &body); err != nil {
		t.Fatalf("unmarshal update body: %v", err)
	}
	if len(body.Transactions) != 1 {
		t.Fatalf("got %d transactions in update body, want 1", len(body.Transactions))
	}
	if body.Transactions[0].CategoryID != "cat-1" {
		t.Errorf("CategoryID = %q, want cat-1", body.Transactions[0].CategoryID)
	}
	if body.Transactions[0].DestinationID != "acc-real" {
		t.Errorf("DestinationID = %q, want acc-real", body.Transactions[0].DestinationID)
	}
}

func TestRunWithOptionsRejectsAccountNamedUnknown(t *testing.T) {
	var lastBody []byte
	categories := []firefly.Category{{ID: "cat-1", Name: "Supermercado"}}
	accounts := []firefly.Account{
		{ID: "29", Name: "unknown"},
		{ID: "acc-real", Name: "Real Store"},
	}

	desc := "Weekly shopping"
	histTxns := []firefly.Transaction{
		{ID: "old-1", Splits: []firefly.Split{{
			JournalID: "j-old-1", Type: "withdrawal", Description: desc,
			DestinationName: "Old Payee", DestinationID: "29", Amount: "42.50",
			CategoryName: "Supermercado", CategoryID: "cat-1",
		}}},
		{ID: "old-2", Splits: []firefly.Split{{
			JournalID: "j-old-2", Type: "withdrawal", Description: desc,
			DestinationName: "Old Payee", DestinationID: "29", Amount: "42.50",
			CategoryName: "Supermercado", CategoryID: "cat-1",
		}}},
		{ID: "old-3", Splits: []firefly.Split{{
			JournalID: "j-old-3", Type: "withdrawal", Description: desc,
			DestinationName: "Old Payee", DestinationID: "29", Amount: "42.50",
			CategoryName: "Supermercado", CategoryID: "cat-1",
		}}},
	}

	server := newMockFireflyServer(t, categories, accounts, nil, histTxns, &lastBody)
	defer server.Close()

	fc := firefly.New(server.URL, "token", "ai")
	reg := job.NewRegistry()
	ca := cache.New(fc, 0, 1)

	amount := 42.50
	j := reg.Create("txn-new", "", "Old Payee", desc, &amount)
	txn := firefly.Transaction{
		ID: "txn-new",
		Splits: []firefly.Split{{
			JournalID:       "j-new",
			Description:     desc,
			DestinationName: "Old Payee",
			Amount:          "42.50",
		}},
	}

	cl := &assertingClassifier{
		t:                   t,
		wantDestinationName: "Old Payee",
		wantDestinationOnly: true,
		result: classifier.Result{
			Outcome:  classifier.Classified,
			Category: "Ignored",
			Destination: &classifier.DestinationResult{
				Name:       "Real Store",
				Action:     "MATCH",
				ID:         "acc-real",
				Confidence: "CLASSIFIED",
			},
		},
	}
	pipe := New(fc, cl, ca, reg, 1, true, false)

	if err := pipe.RunWithOptions(context.Background(), j, txn, RunOptions{ClassifyCategory: true, MatchDestination: true}); err != nil {
		t.Fatalf("RunWithOptions: %v", err)
	}

	if !cl.called {
		t.Fatalf("classifier was not called; unknown-named account must not be used for history match")
	}

	var body struct {
		Transactions []struct {
			DestinationID string `json:"destination_id"`
		} `json:"transactions"`
	}
	if err := json.Unmarshal(lastBody, &body); err != nil {
		t.Fatalf("unmarshal update body: %v", err)
	}
	if body.Transactions[0].DestinationID != "acc-real" {
		t.Errorf("DestinationID = %q, want acc-real", body.Transactions[0].DestinationID)
	}
}

func TestAutoBudgetFromHistoryMatch(t *testing.T) {
	var lastBody []byte
	categories := []firefly.Category{{ID: "cat-1", Name: "Supermercado"}}
	budgets := []firefly.Budget{{ID: "budget-1", Name: "Supermercado"}}
	accounts := []firefly.Account{{ID: "acc-1", Name: "Supermarket"}}

	desc := "Weekly shopping"
	histTxns := []firefly.Transaction{
		{ID: "old-1", Splits: []firefly.Split{{
			JournalID: "j-old-1", Type: "withdrawal", Description: desc,
			DestinationName: "Supermarket", DestinationID: "acc-1", Amount: "42.50",
			CategoryName: "Supermercado", CategoryID: "cat-1",
		}}},
		{ID: "old-2", Splits: []firefly.Split{{
			JournalID: "j-old-2", Type: "withdrawal", Description: desc,
			DestinationName: "Supermarket", DestinationID: "acc-1", Amount: "42.50",
			CategoryName: "Supermercado", CategoryID: "cat-1",
		}}},
		{ID: "old-3", Splits: []firefly.Split{{
			JournalID: "j-old-3", Type: "withdrawal", Description: desc,
			DestinationName: "Supermarket", DestinationID: "acc-1", Amount: "42.50",
			CategoryName: "Supermercado", CategoryID: "cat-1",
		}}},
	}

	server := newMockFireflyServer(t, categories, accounts, budgets, histTxns, &lastBody)
	defer server.Close()

	fc := firefly.New(server.URL, "token", "ai")
	reg := job.NewRegistry()
	ca := cache.New(fc, 0, 1)

	amount := 42.50
	j := reg.Create("txn-new", "", "Supermarket", desc, &amount)
	txn := firefly.Transaction{
		ID: "txn-new",
		Splits: []firefly.Split{{
			JournalID:       "j-new",
			Description:     desc,
			DestinationName: "Supermarket",
			Amount:          "42.50",
		}},
	}

	cl := &assertingClassifier{
		t:                   t,
		wantDestinationName: "",
		wantDestinationOnly: true,
		result: classifier.Result{
			Outcome:  classifier.Classified,
			Category: "Ignored",
		},
	}
	pipe := New(fc, cl, ca, reg, 1, true, true)

	if err := pipe.RunWithOptions(context.Background(), j, txn, RunOptions{ClassifyCategory: true, MatchDestination: true}); err != nil {
		t.Fatalf("RunWithOptions: %v", err)
	}

	if cl.called {
		t.Fatalf("classifier was called; transaction should have been auto-matched from history")
	}

	var body struct {
		Transactions []struct {
			CategoryID string `json:"category_id"`
			BudgetID   string `json:"budget_id"`
		} `json:"transactions"`
	}
	if err := json.Unmarshal(lastBody, &body); err != nil {
		t.Fatalf("unmarshal update body: %v", err)
	}
	if len(body.Transactions) != 1 {
		t.Fatalf("got %d transactions in update body, want 1", len(body.Transactions))
	}
	if body.Transactions[0].CategoryID != "cat-1" {
		t.Errorf("CategoryID = %q, want cat-1", body.Transactions[0].CategoryID)
	}
	if body.Transactions[0].BudgetID != "budget-1" {
		t.Errorf("BudgetID = %q, want budget-1", body.Transactions[0].BudgetID)
	}
}

func TestAutoBudgetFromLLMClassification(t *testing.T) {
	var lastBody []byte
	categories := []firefly.Category{{ID: "cat-1", Name: "Supermercado"}}
	budgets := []firefly.Budget{{ID: "budget-1", Name: "supermercado"}}
	accounts := []firefly.Account{{ID: "acc-1", Name: "Supermarket"}}

	server := newMockFireflyServer(t, categories, accounts, budgets, nil, &lastBody)
	defer server.Close()

	fc := firefly.New(server.URL, "token", "ai")
	reg := job.NewRegistry()
	ca := cache.New(fc, 0, 1)

	amount := 42.50
	j := reg.Create("txn-llm", "", "Supermarket", "Weekly shopping", &amount)
	txn := firefly.Transaction{
		ID: "txn-llm",
		Splits: []firefly.Split{{
			JournalID:       "j-llm",
			Description:     "Weekly shopping",
			DestinationName: "Supermarket",
			Amount:          "42.50",
		}},
	}

	cl := &recordingClassifier{
		t:                   t,
		wantDestinationName: "Supermarket",
		result: classifier.Result{
			Outcome:  classifier.Classified,
			Category: "Supermercado",
		},
	}
	pipe := New(fc, cl, ca, reg, 1, false, true)

	if err := pipe.RunWithOptions(context.Background(), j, txn, RunOptions{ClassifyCategory: true, MatchDestination: false}); err != nil {
		t.Fatalf("RunWithOptions: %v", err)
	}

	var body struct {
		Transactions []struct {
			CategoryID string `json:"category_id"`
			BudgetID   string `json:"budget_id"`
		} `json:"transactions"`
	}
	if err := json.Unmarshal(lastBody, &body); err != nil {
		t.Fatalf("unmarshal update body: %v", err)
	}
	if len(body.Transactions) != 1 {
		t.Fatalf("got %d transactions in update body, want 1", len(body.Transactions))
	}
	if body.Transactions[0].CategoryID != "cat-1" {
		t.Errorf("CategoryID = %q, want cat-1", body.Transactions[0].CategoryID)
	}
	if body.Transactions[0].BudgetID != "budget-1" {
		t.Errorf("BudgetID = %q, want budget-1", body.Transactions[0].BudgetID)
	}
}

func TestAutoBudgetDisabledSkipsAssignment(t *testing.T) {
	var lastBody []byte
	categories := []firefly.Category{{ID: "cat-1", Name: "Supermercado"}}
	budgets := []firefly.Budget{{ID: "budget-1", Name: "Supermercado"}}
	accounts := []firefly.Account{{ID: "acc-1", Name: "Supermarket"}}

	server := newMockFireflyServer(t, categories, accounts, budgets, nil, &lastBody)
	defer server.Close()

	fc := firefly.New(server.URL, "token", "ai")
	reg := job.NewRegistry()
	ca := cache.New(fc, 0, 1)

	amount := 42.50
	j := reg.Create("txn-off", "", "Supermarket", "Weekly shopping", &amount)
	txn := firefly.Transaction{
		ID: "txn-off",
		Splits: []firefly.Split{{
			JournalID:       "j-off",
			Description:     "Weekly shopping",
			DestinationName: "Supermarket",
			Amount:          "42.50",
		}},
	}

	cl := &recordingClassifier{
		t:                   t,
		wantDestinationName: "Supermarket",
		result: classifier.Result{
			Outcome:  classifier.Classified,
			Category: "Supermercado",
		},
	}
	pipe := New(fc, cl, ca, reg, 1, false, false)

	if err := pipe.RunWithOptions(context.Background(), j, txn, RunOptions{ClassifyCategory: true, MatchDestination: false}); err != nil {
		t.Fatalf("RunWithOptions: %v", err)
	}

	var body struct {
		Transactions []struct {
			CategoryID string `json:"category_id"`
			BudgetID   string `json:"budget_id"`
		} `json:"transactions"`
	}
	if err := json.Unmarshal(lastBody, &body); err != nil {
		t.Fatalf("unmarshal update body: %v", err)
	}
	if len(body.Transactions) != 1 {
		t.Fatalf("got %d transactions in update body, want 1", len(body.Transactions))
	}
	if body.Transactions[0].BudgetID != "" {
		t.Errorf("BudgetID = %q, want empty when auto-budget is disabled", body.Transactions[0].BudgetID)
	}
}

func TestAutoBudgetNoMatchLeavesBudgetEmpty(t *testing.T) {
	var lastBody []byte
	categories := []firefly.Category{{ID: "cat-1", Name: "Supermercado"}}
	budgets := []firefly.Budget{{ID: "budget-1", Name: "Hogar"}}
	accounts := []firefly.Account{{ID: "acc-1", Name: "Supermarket"}}

	server := newMockFireflyServer(t, categories, accounts, budgets, nil, &lastBody)
	defer server.Close()

	fc := firefly.New(server.URL, "token", "ai")
	reg := job.NewRegistry()
	ca := cache.New(fc, 0, 1)

	amount := 42.50
	j := reg.Create("txn-nomatch", "", "Supermarket", "Weekly shopping", &amount)
	txn := firefly.Transaction{
		ID: "txn-nomatch",
		Splits: []firefly.Split{{
			JournalID:       "j-nomatch",
			Description:     "Weekly shopping",
			DestinationName: "Supermarket",
			Amount:          "42.50",
		}},
	}

	cl := &recordingClassifier{
		t:                   t,
		wantDestinationName: "Supermarket",
		result: classifier.Result{
			Outcome:  classifier.Classified,
			Category: "Supermercado",
		},
	}
	pipe := New(fc, cl, ca, reg, 1, false, true)

	if err := pipe.RunWithOptions(context.Background(), j, txn, RunOptions{ClassifyCategory: true, MatchDestination: false}); err != nil {
		t.Fatalf("RunWithOptions: %v", err)
	}

	var body struct {
		Transactions []struct {
			CategoryID string `json:"category_id"`
			BudgetID   string `json:"budget_id"`
		} `json:"transactions"`
	}
	if err := json.Unmarshal(lastBody, &body); err != nil {
		t.Fatalf("unmarshal update body: %v", err)
	}
	if len(body.Transactions) != 1 {
		t.Fatalf("got %d transactions in update body, want 1", len(body.Transactions))
	}
	if body.Transactions[0].BudgetID != "" {
		t.Errorf("BudgetID = %q, want empty when no budget name matches", body.Transactions[0].BudgetID)
	}
}

func floatPtr(v float64) *float64 {
	return &v
}
