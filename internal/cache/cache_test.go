package cache

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/openaccountants/firefly-iii-ai-categorize/internal/classifier"
	"github.com/openaccountants/firefly-iii-ai-categorize/internal/firefly"
)

func TestRefreshStripsUnknownDestinations(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/transactions" {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		json.NewEncoder(w).Encode(map[string]interface{}{
			"data": []map[string]interface{}{{
				"id": "txn-old",
				"attributes": map[string]interface{}{
					"group_title": "",
					"transactions": []map[string]interface{}{{
						"transaction_journal_id": "j-1",
						"type":                   "withdrawal",
						"description":            "Weekly shopping",
						"destination_name":       "unknown",
						"destination_id":         "29",
						"amount":                 "42.50",
						"category_name":          "Groceries",
						"category_id":            "cat-1",
					}},
				},
			}},
			"meta": map[string]interface{}{"pagination": map[string]interface{}{"total_pages": 1, "total": 1}},
		})
	}))
	defer server.Close()

	fc := firefly.New(server.URL, "token", "ai")
	ca := New(fc, 0, 1)

	history := ca.GetHistory(context.Background(), "Weekly shopping", 10)
	if len(history) != 1 {
		t.Fatalf("got %d history entries, want 1", len(history))
	}

	entry := history[0]
	if entry.DestinationName != "" {
		t.Errorf("DestinationName = %q, want empty", entry.DestinationName)
	}
	if entry.DestinationAccountID != "" {
		t.Errorf("DestinationAccountID = %q, want empty", entry.DestinationAccountID)
	}
	if entry.GroupKey != "Weekly shopping" {
		t.Errorf("GroupKey = %q, want %q", entry.GroupKey, "Weekly shopping")
	}
	if entry.CategoryName != "Groceries" {
		t.Errorf("CategoryName = %q, want %q", entry.CategoryName, "Groceries")
	}
}

func TestRefreshKeepsKnownDestinations(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"data": []map[string]interface{}{{
				"id": "txn-known",
				"attributes": map[string]interface{}{
					"group_title": "",
					"transactions": []map[string]interface{}{{
						"transaction_journal_id": "j-2",
						"type":                   "withdrawal",
						"description":            "Fuel",
						"destination_name":       "Shell",
						"destination_id":         "7",
						"amount":                 "60.00",
						"category_name":          "Transport",
						"category_id":            "cat-2",
					}},
				},
			}},
			"meta": map[string]interface{}{"pagination": map[string]interface{}{"total_pages": 1, "total": 1}},
		})
	}))
	defer server.Close()

	fc := firefly.New(server.URL, "token", "ai")
	ca := New(fc, 0, 1)

	history := ca.GetHistory(context.Background(), classifier.GroupKey("Shell", "Fuel"), 10)
	if len(history) != 1 {
		t.Fatalf("got %d history entries, want 1", len(history))
	}

	entry := history[0]
	if entry.DestinationName != "Shell" {
		t.Errorf("DestinationName = %q, want Shell", entry.DestinationName)
	}
	if entry.DestinationAccountID != "7" {
		t.Errorf("DestinationAccountID = %q, want 7", entry.DestinationAccountID)
	}
}
