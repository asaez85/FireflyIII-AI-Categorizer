package classifier

import (
	"encoding/json"
	"testing"
)

func makeRaw(name, action, confidence string) []byte {
	r := rawResponse{
		Outcome:     string(Classified),
		Category:    json.RawMessage(`"Groceries"`),
		Reason:      "ok",
		Destination: &rawDestination{Name: name, Action: action, Confidence: confidence},
	}
	b, _ := json.Marshal(r)
	return b
}

var testCategories = []Category{{Name: "Groceries"}}

func TestParseResponseDestinationMatchCanonicalizesName(t *testing.T) {
	candidates := []AccountCandidate{
		{ID: "pp-1", Name: "PayPal Europe S.a.r.l. et Cie S.C.A"},
		{ID: "pp-2", Name: "PayPal (Europe) S.a r.l. et Cie, S.C.A."},
	}
	raw := makeRaw("PayPal Europe S.a.r.l. et Cie, S.C.A.", "MATCH", "CLASSIFIED")
	res := parseResponse(string(raw), "prompt", testCategories, candidates, true)

	if res.Outcome != Classified {
		t.Fatalf("unexpected outcome: %v", res.Outcome)
	}
	if res.Destination == nil {
		t.Fatal("expected destination, got nil")
	}
	if res.Destination.ID != "pp-1" {
		t.Errorf("ID = %q, want pp-1", res.Destination.ID)
	}
	if res.Destination.Name != "PayPal Europe S.a.r.l. et Cie S.C.A" {
		t.Errorf("Name = %q, want canonical 'PayPal Europe S.a.r.l. et Cie S.C.A'", res.Destination.Name)
	}
}

func TestParseResponseDestinationMatchSecondCandidate(t *testing.T) {
	candidates := []AccountCandidate{
		{ID: "pp-2", Name: "PayPal (Europe) S.a r.l. et Cie, S.C.A."},
	}
	raw := makeRaw("PayPal Europe S.a.r.l. et Cie, S.C.A.", "MATCH", "CLASSIFIED")
	res := parseResponse(string(raw), "prompt", testCategories, candidates, true)

	if res.Destination == nil {
		t.Fatal("expected destination, got nil")
	}
	if res.Destination.ID != "pp-2" {
		t.Errorf("ID = %q, want pp-2", res.Destination.ID)
	}
	if res.Destination.Name != "PayPal (Europe) S.a r.l. et Cie, S.C.A." {
		t.Errorf("Name = %q, want canonical candidate name", res.Destination.Name)
	}
}

func TestParseResponseDestinationExactMatch(t *testing.T) {
	candidates := []AccountCandidate{
		{ID: "amz", Name: "Amazon"},
	}
	raw := makeRaw("AMAZON", "MATCH", "CLASSIFIED")
	res := parseResponse(string(raw), "prompt", testCategories, candidates, true)

	if res.Destination == nil {
		t.Fatal("expected destination, got nil")
	}
	if res.Destination.ID != "amz" || res.Destination.Name != "Amazon" {
		t.Errorf("got %+v, want ID=amz Name=Amazon", res.Destination)
	}
}

func TestParseResponseDestinationCreateUnchanged(t *testing.T) {
	candidates := []AccountCandidate{
		{ID: "old", Name: "Old Vendor"},
	}
	raw := makeRaw("New Vendor Inc.", "CREATE", "CLASSIFIED")
	res := parseResponse(string(raw), "prompt", testCategories, candidates, true)

	if res.Destination == nil {
		t.Fatal("expected destination, got nil")
	}
	if res.Destination.ID != "" {
		t.Errorf("CREATE should not set ID, got %q", res.Destination.ID)
	}
	if res.Destination.Name != "New Vendor Inc." {
		t.Errorf("CREATE name changed to %q", res.Destination.Name)
	}
}

func TestParseResponseDestinationNoMatchBecomesNil(t *testing.T) {
	candidates := []AccountCandidate{
		{ID: "amz", Name: "Amazon"},
	}
	raw := makeRaw("Totally Unknown Shop", "MATCH", "CLASSIFIED")
	res := parseResponse(string(raw), "prompt", testCategories, candidates, true)

	if res.Destination != nil {
		t.Errorf("expected nil destination for no match, got %+v", res.Destination)
	}
}

func TestParseResponseDestinationInvalidActionIgnored(t *testing.T) {
	candidates := []AccountCandidate{
		{ID: "amz", Name: "Amazon"},
	}
	raw := makeRaw("Amazon", "DELETE", "CLASSIFIED")
	res := parseResponse(string(raw), "prompt", testCategories, candidates, true)

	if res.Destination != nil {
		t.Errorf("expected nil destination for invalid action, got %+v", res.Destination)
	}
}

func TestParseResponseDestinationInvalidConfidenceIgnored(t *testing.T) {
	candidates := []AccountCandidate{
		{ID: "amz", Name: "Amazon"},
	}
	raw := makeRaw("Amazon", "MATCH", "GUESS")
	res := parseResponse(string(raw), "prompt", testCategories, candidates, true)

	if res.Destination != nil {
		t.Errorf("expected nil destination for invalid confidence, got %+v", res.Destination)
	}
}

func TestMatchAccountCandidatePriority(t *testing.T) {
	candidates := []AccountCandidate{
		{ID: "exact", Name: "PayPal"},
		{ID: "clean", Name: "Pay Pal"},
		{ID: "alpha", Name: "PayPal1"},
	}

	// Exact match wins even though clean would also match a different candidate.
	if m, ok := MatchAccountCandidate("paypal", candidates); !ok || m.ID != "exact" {
		t.Errorf("exact match failed: got %+v, %v", m, ok)
	}

	// Clean normalized match (punctuation/whitespace tolerant).
	if m, ok := MatchAccountCandidate("PAY-PAL", candidates); !ok || m.ID != "clean" {
		t.Errorf("clean match failed: got %+v, %v", m, ok)
	}

	// Alphanumeric-only match.
	if m, ok := MatchAccountCandidate("PAYPAL1", candidates); !ok || m.ID != "alpha" {
		t.Errorf("alphanumeric match failed: got %+v, %v", m, ok)
	}

	// No match.
	if _, ok := MatchAccountCandidate("Visa", candidates); ok {
		t.Error("expected no match for Visa")
	}
}

func TestNormalizeAccountNameDiacriticsAndPunctuation(t *testing.T) {
	got := NormalizeAccountName("PayPal (Europe) S.a r.l. et Cie, S.C.A.")
	want := "paypal europe s a r l et cie s c a"
	if got != want {
		t.Errorf("NormalizeAccountName = %q, want %q", got, want)
	}
}

func TestNormalizeAccountNameAlphaNum(t *testing.T) {
	got := NormalizeAccountNameAlphaNum("PayPal (Europe) S.a r.l. et Cie, S.C.A.")
	want := "paypaleuropesarletciesca"
	if got != want {
		t.Errorf("NormalizeAccountNameAlphaNum = %q, want %q", got, want)
	}
}

func TestParseResponseDestinationDisabled(t *testing.T) {
	candidates := []AccountCandidate{
		{ID: "amz", Name: "Amazon"},
	}
	raw := makeRaw("Amazon", "MATCH", "CLASSIFIED")
	res := parseResponse(string(raw), "prompt", testCategories, candidates, false)

	if res.Destination != nil {
		t.Errorf("expected nil destination when disabled, got %+v", res.Destination)
	}
}
