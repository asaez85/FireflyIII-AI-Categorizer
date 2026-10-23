package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/openaccountants/firefly-iii-ai-categorize/internal/config"
	"github.com/openaccountants/firefly-iii-ai-categorize/internal/firefly"
)

func TestMergeConfigUpdateJev(t *testing.T) {
	on, high, low := true, 0.9, 0.3
	key, model := "or-key", "typesafe/jev-2"
	got := mergeConfigUpdate(config.StoredConfig{OpenRouterKey: "old"}, configUpdateRequest{
		JevEnabled: &on, OpenRouterKey: &key, JevModel: &model, JevHighThreshold: &high, JevLowThreshold: &low,
	})
	if got.JevEnabled == nil || !*got.JevEnabled || got.OpenRouterKey != "or-key" || got.JevModel != model ||
		got.JevHighThreshold == nil || *got.JevHighThreshold != 0.9 || got.JevLowThreshold == nil || *got.JevLowThreshold != 0.3 {
		t.Errorf("merged = %+v", got)
	}

	empty := ""
	got = mergeConfigUpdate(config.StoredConfig{OpenRouterKey: "old"}, configUpdateRequest{OpenRouterKey: &empty})
	if got.OpenRouterKey != "old" {
		t.Errorf("empty key must keep existing, got %q", got.OpenRouterKey)
	}
}

func TestPutConfigRejectsInvalidThresholds(t *testing.T) {
	store, err := config.NewStore(t.TempDir() + "/config.json")
	if err != nil {
		t.Fatal(err)
	}
	h := &Handler{
		baseCfg: &config.Config{JevHighThreshold: 0.85, JevLowThreshold: 0.5},
		store:   store,
	}

	for _, body := range []string{
		`{"jev_high_threshold":0.4,"jev_low_threshold":0.6}`,
		`{"jev_high_threshold":1.5}`,
		`{"jev_low_threshold":-0.1}`,
	} {
		rec := httptest.NewRecorder()
		h.putConfig(rec, httptest.NewRequest(http.MethodPut, "/api/config", strings.NewReader(body)))
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%s: status = %d, want 400", body, rec.Code)
		}
		if got := store.Get(); got.JevHighThreshold != nil || got.JevLowThreshold != nil {
			t.Errorf("%s: invalid thresholds were persisted: %+v", body, got)
		}
	}
}

func TestMergeConfigUpdateEmptyJevModelResets(t *testing.T) {
	empty := ""
	got := mergeConfigUpdate(config.StoredConfig{JevModel: "typesafe/jev-2"}, configUpdateRequest{JevModel: &empty})
	if got.JevModel != "" {
		t.Errorf("JevModel = %q, want cleared so the env/default model applies", got.JevModel)
	}
}

func TestMatchesTxnFilterUnknownDestination(t *testing.T) {
	rows := []firefly.TransactionRow{
		{ID: "1", DestinationName: ""},
		{ID: "2", DestinationName: "(no name)"},
		{ID: "3", DestinationName: "(unknown destination)"},
		{ID: "4", DestinationName: "Amazon"},
	}
	want := []string{"1", "2", "3"}
	var got []string
	for _, r := range rows {
		if matchesTxnFilter(r, false, true, "", "") {
			got = append(got, r.ID)
		}
	}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("got %v, want %v", got, want)
		}
	}
}
