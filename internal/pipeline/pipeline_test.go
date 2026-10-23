package pipeline

import (
	"context"
	"encoding/json"
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
	pipe := New(fc, cl, ca, reg, 1, true)

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
	pipe := New(fc, cl, ca, reg, 1, false)

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
	pipe := New(fc, cl, ca, reg, 1, true)

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
