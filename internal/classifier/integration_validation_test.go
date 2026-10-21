package classifier

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

// Real Firefly III categories from asaez85's instance (sample)
var realCategories = []Category{
	{Name: "Agua"},
	{Name: "Electricidad"},
	{Name: "Electrónica"},
	{Name: "Espectáculos"},
	{Name: "Gasolina"},
	{Name: "Hogar"},
	{Name: "Juegos"},
	{Name: "Móvil"},
	{Name: "Restaurante"},
	{Name: "Ropa"},
	{Name: "Supermercado"},
	{Name: "Suscripciones"},
	{Name: "Transportes"},
}

// Real expense accounts from asaez85's instance (sample)
var realExpenseAccounts = []AccountCandidate{
	{ID: "878", Name: "Pago Amazon *Mktplce EU ES"},
	{ID: "900", Name: "CARREFOUR GRVIA"},
	{ID: "910", Name: "YELMO CINES PUERTA DE ALI"},
	{ID: "920", Name: "SUMUP  *LA FAMILIA"},
	{ID: "930", Name: "PEPE ENERGY"},
	{ID: "940", Name: "OLLAMA"},
	{ID: "950", Name: "PayPal Europe S.a.r.l. et Cie S.C.A"},
}

func realRequest(desc, destName string, amount float64) Request {
	return Request{
		Description:         desc,
		DestinationName:     destName,
		Amount:              &amount,
		Categories:          realCategories,
		ExpenseAccounts:     realExpenseAccounts,
		DestinationMatching: true,
		History: []HistoricalEntry{
			{Description: "PAGO CON TARJETA EN GRANDES SUPERFICIES // CARREFOUR", CategoryName: "Supermercado", Amount: 42.50},
			{Description: "PAGO CON TARJETA EN GRANDES SUPERFICIES // CARREFOUR", CategoryName: "Supermercado", Amount: 38.20},
			{Description: "PAGO CON TARJETA EN DISCOS, LIBROS, FOTOS // OLLAMA", CategoryName: "Suscripciones", Amount: 18.54},
		},
	}
}

// TestJEVQueryBuildsValidRequest verifies the query structure matches what
// OpenRouter's Decisions API expects, with real category names.
func TestJEVQueryBuildsValidRequest(t *testing.T) {
	req := realRequest("PAGO CON TARJETA EN GRANDES SUPERFICIES // CARREFOUR GRVIA", "CARREFOUR GRVIA", 43.42)
	q := buildJevQuery(req, "Alberto, Spanish user, lives in Valencia")

	// Verify state
	state, ok := q.req.State.(jevState)
	if !ok {
		t.Fatalf("state type = %T, want jevState", q.req.State)
	}
	if state.Transaction.Destination != "CARREFOUR GRVIA" {
		t.Errorf("destination = %q", state.Transaction.Destination)
	}
	if state.Transaction.Description != req.Description {
		t.Errorf("description mismatch")
	}
	if state.Transaction.Amount == nil || *state.Transaction.Amount != 43.42 {
		t.Errorf("amount = %v", state.Transaction.Amount)
	}
	if state.AccountHolderContext != "Alberto, Spanish user, lives in Valencia" {
		t.Errorf("context = %q", state.AccountHolderContext)
	}
	if len(state.History) != 3 {
		t.Errorf("history entries = %d, want 3", len(state.History))
	}

	// Verify category question
	cq, ok := q.req.Questions["category"]
	if !ok {
		t.Fatal("no category question")
	}
	if cq.Type != "choice" {
		t.Errorf("category type = %q, want choice", cq.Type)
	}
	if len(cq.Criteria) != len(realCategories) {
		t.Errorf("category criteria = %d, want %d", len(cq.Criteria), len(realCategories))
	}

	// Verify each category maps to a real name
	for key, cat := range q.categories {
		if criterion, ok := cq.Criteria[key]; !ok {
			t.Errorf("category %s missing from criteria", cat.Name)
		} else if !strings.Contains(criterion, cat.Name) {
			t.Errorf("criterion %q does not contain category name %q", criterion, cat.Name)
		}
	}

	// Verify destination question exists (DestinationMatching=true, not CategoryOnly)
	dq, ok := q.req.Questions["destination"]
	if !ok {
		t.Fatal("no destination question")
	}
	if len(dq.Criteria) == 0 {
		t.Error("destination criteria is empty")
	}
	if _, hasNew := dq.Criteria[newAccountKey]; !hasNew {
		t.Error("destination criteria missing 'new' option")
	}
}

// TestJEVInterpretWithRealCategories verifies that JEV's synthetic keys (c0, c1, ...)
// correctly map back to real Firefly III category names.
func TestJEVInterpretWithRealCategories(t *testing.T) {
	req := realRequest("PAGO CON TARJETA EN GRANDES SUPERFICIES // CARREFOUR GRVIA", "CARREFOUR GRVIA", 43.42)
	q := buildJevQuery(req, "")

	// Find the key for "Supermercado"
	var supermercadoKey string
	for key, cat := range q.categories {
		if cat.Name == "Supermercado" {
			supermercadoKey = key
			break
		}
	}
	if supermercadoKey == "" {
		t.Fatal("Supermercado not in categories")
	}

	// Simulate JEV response: high confidence Supermercado
	resp := decisionResponse{
		Model: "typesafe/jev-1.13",
		Answers: map[string]choiceAnswer{
			"category": {
				Type:   "choice",
				Choice: supermercadoKey,
				Probabilities: map[string]float64{
					supermercadoKey: 0.94,
				},
			},
		},
	}

	d, err := q.interpret(resp, DefaultThresholds)
	if err != nil {
		t.Fatalf("interpret error: %v", err)
	}
	if d.category.Name != "Supermercado" {
		t.Errorf("category = %q, want Supermercado", d.category.Name)
	}
	if d.categoryP != 0.94 {
		t.Errorf("categoryP = %f, want 0.94", d.categoryP)
	}
}

// TestJEVInterpretWithRealDestinations verifies destination mapping.
func TestJEVInterpretWithRealDestinations(t *testing.T) {
	req := realRequest("PAGO TARJETA AMAZON", "Pago Amazon *Mktplce EU ES", 75.89)
	q := buildJevQuery(req, "")

	// Find keys
	var supermercadoKey string
	for key, cat := range q.categories {
		if cat.Name == "Supermercado" {
			supermercadoKey = key
		}
	}
	if supermercadoKey == "" {
		t.Fatal("Supermercado not found")
	}

	// Simulate JEV: high confidence category + high confidence destination match
	resp := decisionResponse{
		Model: "typesafe/jev-1.13",
		Answers: map[string]choiceAnswer{
			"category": {
				Type:   "choice",
				Choice: supermercadoKey,
				Probabilities: map[string]float64{
					supermercadoKey: 0.91,
				},
			},
		},
	}
	if len(q.accounts) > 0 {
		for key, acct := range q.accounts {
			if acct.Name == "CARREFOUR GRVIA" {
				resp.Answers["destination"] = choiceAnswer{
					Type:   "choice",
					Choice: key,
					Probabilities: map[string]float64{
						key: 0.97,
					},
				}
			}
		}
	}

	d, err := q.interpret(resp, DefaultThresholds)
	if err != nil {
		t.Fatalf("interpret error: %v", err)
	}
	if d.category.Name != "Supermercado" {
		t.Errorf("category = %q", d.category.Name)
	}
	if d.categoryP < DefaultThresholds.High {
		t.Errorf("categoryP = %f, want >= %f", d.categoryP, DefaultThresholds.High)
	}
}

// TestCascadeEndToEndWithRealData runs the full cascade with a fake JEV
// that returns realistic responses for real Firefly III transactions.
func TestCascadeEndToEndWithRealData(t *testing.T) {
	tests := []struct {
		name         string
		desc         string
		destName     string
		amount       float64
		jevCatLabel  string
		jevCatProb   float64
		jevDestLabel string
		jevDestProb  float64
		wantOutcome  Outcome
		wantCategory string
		wantDestName string
		wantLLMCalls int
	}{
		{
			name:         "Carrefour high confidence -> CLASSIFIED, no LLM",
			desc:         "PAGO CON TARJETA EN GRANDES SUPERFICIES // CARREFOUR GRVIA",
			destName:     "CARREFOUR GRVIA",
			amount:       43.42,
			jevCatLabel:  "Supermercado",
			jevCatProb:   0.95,
			jevDestLabel: "CARREFOUR GRVIA",
			jevDestProb:  0.97,
			wantOutcome:  Classified,
			wantCategory: "Supermercado",
			wantDestName: "CARREFOUR GRVIA",
			wantLLMCalls: 0,
		},
		{
			name:         "Restaurant middle band -> LLM escalation",
			desc:         "PAGO CON TARJETA EN RESTAURANTES Y CAFETERIAS // SUMUP *LA FAMILIA",
			destName:     "SUMUP  *LA FAMILIA",
			amount:       30.00,
			jevCatLabel:  "Restaurante",
			jevCatProb:   0.65,
			jevDestLabel: "SUMUP  *LA FAMILIA",
			jevDestProb:  0.90,
			wantOutcome:  Assumed,
			wantCategory: "Restaurante",
			wantDestName: "SUMUP  *LA FAMILIA",
			wantLLMCalls: 1,
		},
		{
			name:         "Unknown payee low confidence -> NEEDS_REVIEW",
			desc:         "ADEUDO SEPA Misterioso // PAGO DESCONOCIDO",
			destName:     "MISTERIOSO PAYEE",
			amount:       99.99,
			jevCatLabel:  "Supermercado",
			jevCatProb:   0.30,
			jevDestLabel: "",
			jevDestProb:  0,
			wantOutcome:  NeedsReview,
			wantCategory: "",
			wantDestName: "",
			wantLLMCalls: 0,
		},
		{
			name:         "Electronics high confidence -> CLASSIFIED",
			desc:         "Amazon - Teclado Logitech",
			destName:     "Pago Amazon *Mktplce EU ES",
			amount:       75.89,
			jevCatLabel:  "Electrónica",
			jevCatProb:   0.92,
			jevDestLabel: "Pago Amazon *Mktplce EU ES",
			jevDestProb:  0.95,
			wantOutcome:  Classified,
			wantCategory: "Electrónica",
			wantDestName: "Pago Amazon *Mktplce EU ES",
			wantLLMCalls: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			jev := &fakeDecider{fn: func(r decisionRequest) (decisionResponse, error) {
				answers := map[string]choiceAnswer{}
				cq := r.Questions["category"]
				catKey := keyFor(t, cq, tt.jevCatLabel)
				answers["category"] = choiceAnswer{
					Type:   "choice",
					Choice: catKey,
					Probabilities: map[string]float64{
						catKey: tt.jevCatProb,
					},
				}
				if tt.jevDestLabel != "" {
					dq := r.Questions["destination"]
					destKey := keyFor(t, dq, tt.jevDestLabel)
					answers["destination"] = choiceAnswer{
						Type:   "choice",
						Choice: destKey,
						Probabilities: map[string]float64{
							destKey: tt.jevDestProb,
						},
					}
				}
				return decisionResponse{Answers: answers, Raw: `{"answers":"ok"}`}, nil
			}}

			llm := &fakeLLM{res: Result{
				Outcome:  Assumed,
				Category: tt.wantCategory,
				Reason:   "llm fallback reason",
				Destination: &DestinationResult{
					Name:       tt.wantDestName,
					Action:     "MATCH",
					Confidence: "CLASSIFIED",
				},
				RawPrompt:   "llm prompt",
				RawResponse: "llm response",
			}}

			req := realRequest(tt.desc, tt.destName, tt.amount)
			res, err := newCascade(llm, jev, DefaultThresholds, "Alberto, Spanish user").Classify(context.Background(), req)
			if err != nil {
				t.Fatalf("Classify error: %v", err)
			}

			if res.Outcome != tt.wantOutcome {
				t.Errorf("Outcome = %v, want %v (reason: %s)", res.Outcome, tt.wantOutcome, res.Reason)
			}
			if tt.wantOutcome == Classified && res.Category != tt.wantCategory {
				t.Errorf("Category = %q, want %q", res.Category, tt.wantCategory)
			}
			if llm.calls != tt.wantLLMCalls {
				t.Errorf("LLM calls = %d, want %d", llm.calls, tt.wantLLMCalls)
			}
			if tt.wantDestName != "" && res.Destination != nil {
				if res.Destination.Name != tt.wantDestName {
					t.Errorf("Destination = %q, want %q", res.Destination.Name, tt.wantDestName)
				}
			}

			if res.RawPrompt == "" {
				t.Error("RawPrompt is empty")
			}
			if res.RawResponse == "" {
				t.Error("RawResponse is empty")
			}

			t.Logf("  Outcome=%s Category=%s Source=%s LLM=%d Reason=%s",
				res.Outcome, res.Category, res.Source, llm.calls, res.Reason)
		})
	}
}

// TestJEVApiResponseParsing verifies JSON parsing of realistic OpenRouter responses.
func TestJEVApiResponseParsing(t *testing.T) {
	rawJSON := `{
		"id": "dec_abc123",
		"model": "typesafe/jev-1.13",
		"answers": {
			"category": {
				"type": "choice",
				"choice": "c3",
				"confidence": 0.92,
				"probabilities": {
					"c0": 0.02,
					"c1": 0.03,
					"c2": 0.03,
					"c3": 0.92
				}
			},
			"destination": {
				"type": "choice",
				"choice": "a1",
				"confidence": 0.88,
				"probabilities": {
					"a0": 0.05,
					"a1": 0.88,
					"new": 0.07
				}
			}
		}
	}`

	var resp decisionResponse
	if err := json.Unmarshal([]byte(rawJSON), &resp); err != nil {
		t.Fatalf("failed to parse: %v", err)
	}

	if resp.ID != "dec_abc123" {
		t.Errorf("ID = %q", resp.ID)
	}
	if resp.Model != "typesafe/jev-1.13" {
		t.Errorf("Model = %q", resp.Model)
	}
	if len(resp.Answers) != 2 {
		t.Errorf("Answers = %d, want 2", len(resp.Answers))
	}

	catAns := resp.Answers["category"]
	if catAns.Choice != "c3" {
		t.Errorf("category choice = %q", catAns.Choice)
	}
	if catAns.Confidence == nil || *catAns.Confidence != 0.92 {
		t.Errorf("category confidence = %v", catAns.Confidence)
	}
	if len(catAns.Probabilities) != 4 {
		t.Errorf("category probabilities = %d, want 4", len(catAns.Probabilities))
	}

	destAns := resp.Answers["destination"]
	if destAns.Choice != "a1" {
		t.Errorf("destination choice = %q", destAns.Choice)
	}
}

// TestJEVResponseWithConfidenceOnly verifies parsing when probabilities are absent
// but confidence is present (fallback path in answerScore).
func TestJEVResponseWithConfidenceOnly(t *testing.T) {
	rawJSON := `{
		"id": "dec_xyz",
		"model": "typesafe/jev-1.13",
		"answers": {
			"category": {
				"type": "choice",
				"choice": "c2",
				"confidence": 0.78
			}
		}
	}`

	var resp decisionResponse
	if err := json.Unmarshal([]byte(rawJSON), &resp); err != nil {
		t.Fatalf("failed to parse: %v", err)
	}

	ans := resp.Answers["category"]
	score, ok := answerScore(ans)
	if !ok {
		t.Fatal("answerScore returned false, want true (confidence fallback)")
	}
	if score != 0.78 {
		t.Errorf("score = %f, want 0.78", score)
	}
}

// TestTagMapping verifies the outcome-to-tag mapping that syncs to Firefly III.
func TestTagMapping(t *testing.T) {
	tagForOutcome := func(prefix, outcome string) string {
		switch outcome {
		case "CLASSIFIED":
			return prefix + ":classified"
		case "ASSUMED":
			return prefix + ":assumed"
		default:
			return prefix + ":needs-review"
		}
	}

	tests := []struct {
		outcome Outcome
		wantTag string
	}{
		{Classified, "ai:classified"},
		{Assumed, "ai:assumed"},
		{NeedsReview, "ai:needs-review"},
	}

	for _, tt := range tests {
		tag := tagForOutcome("ai", string(tt.outcome))
		if tag != tt.wantTag {
			t.Errorf("outcome %s -> tag %q, want %q", tt.outcome, tag, tt.wantTag)
		}
	}
}

// TestTokenCostEstimation verifies cost calculation for JEV API calls.
// JEV pricing: $0.042/M input tokens, $0/M output tokens (free output).
func TestTokenCostEstimation(t *testing.T) {
	req := realRequest("PAGO CON TARJETA EN GRANDES SUPERFICIES // CARREFOUR GRVIA", "CARREFOUR GRVIA", 43.42)
	q := buildJevQuery(req, "Alberto, Spanish user, lives in Valencia")
	q.req.Model = DefaultJevModel

	body, _ := json.Marshal(q.req)
	inputTokens := len(body) / 4 // rough estimate: 4 chars per token

	costUSD := float64(inputTokens) / 1_000_000 * 0.042
	costEUR := costUSD * 0.92

	t.Logf("Request size: %d bytes, est. input tokens: %d", len(body), inputTokens)
	t.Logf("Est. cost per call: $%.6f (≈€%.6f)", costUSD, costEUR)

	dailyCalls := 100
	monthlyTokens := inputTokens * dailyCalls * 30
	monthlyCostUSD := float64(monthlyTokens) / 1_000_000 * 0.042
	t.Logf("Est. monthly cost (100 tx/day): $%.2f", monthlyCostUSD)

	if monthlyCostUSD > 5.0 {
		t.Errorf("Monthly cost $%.2f seems too high for 100 tx/day", monthlyCostUSD)
	}
}

// TestCascadeWithHistoryContext verifies JEV receives account holder context.
func TestCascadeWithHistoryContext(t *testing.T) {
	var capturedState jevState
	jev := &fakeDecider{fn: func(r decisionRequest) (decisionResponse, error) {
		capturedState = r.State.(jevState)
		cq := r.Questions["category"]
		key := "c0"
		for k := range cq.Criteria {
			key = k
			break
		}
		return decisionResponse{
			Answers: map[string]choiceAnswer{
				"category": {Type: "choice", Choice: key, Probabilities: map[string]float64{key: 0.90}},
			},
		}, nil
	}}

	ctx := "Alberto gasta principalmente en: supermercado semanal, gasolina, restaurantes ocasionales. Vive en Valencia, España."
	req := realRequest("Test transaction", "Test payee", 10.0)
	_, err := newCascade(&fakeLLM{}, jev, DefaultThresholds, ctx).Classify(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}

	if capturedState.AccountHolderContext != ctx {
		t.Errorf("AccountHolderContext = %q, want %q", capturedState.AccountHolderContext, ctx)
	}
	if len(capturedState.History) != 3 {
		t.Errorf("History entries = %d, want 3", len(capturedState.History))
	}
	for i, h := range capturedState.History {
		t.Logf("  History[%d]: %s -> %s", i, h.Description, h.Category)
	}
}

// TestCascadeNewAccountFlowWithRealData verifies the "new payee" flow.
func TestCascadeNewAccountFlowWithRealData(t *testing.T) {
	jev := &fakeDecider{fn: func(r decisionRequest) (decisionResponse, error) {
		answers := map[string]choiceAnswer{}
		cq := r.Questions["category"]
		catKey := "c0"
		for k, crit := range cq.Criteria {
			if crit == "Electrónica" || strings.HasPrefix(crit, "Electrónica") {
				catKey = k
				break
			}
		}
		answers["category"] = choiceAnswer{Type: "choice", Choice: catKey, Probabilities: map[string]float64{catKey: 0.93}}
		answers["destination"] = choiceAnswer{Type: "choice", Choice: newAccountKey, Probabilities: map[string]float64{newAccountKey: 0.91}}
		return decisionResponse{Answers: answers}, nil
	}}

	llm := &fakeLLM{res: Result{
		Outcome:  Classified,
		Category: "Electrónica",
		Destination: &DestinationResult{
			Name:       "Pago Amazon *Mktplce EU ES",
			Action:     "CREATE",
			Confidence: "CLASSIFIED",
		},
		RawPrompt:   "llm prompt",
		RawResponse: "llm response",
	}}

	req := realRequest("Amazon - Teclado Logitech", "Pago Amazon *Mktplce EU ES", 75.89)
	req.CategoryOnly = false

	res, err := newCascade(llm, jev, DefaultThresholds, "").Classify(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if res.Category != "Electrónica" {
		t.Errorf("Category = %q, want Electrónica", res.Category)
	}
	if res.Destination == nil || res.Destination.Action != "CREATE" {
		t.Errorf("Destination = %+v, want CREATE", res.Destination)
	}
	if res.Destination != nil && res.Destination.Name != "Pago Amazon *Mktplce EU ES" {
		t.Errorf("Destination name = %q", res.Destination.Name)
	}
	if res.Source != SourceJevLLM {
		t.Errorf("Source = %q, want jev+llm", res.Source)
	}
	fmt.Printf("  New account flow: Category=%s, Dest=%s (%s), Source=%s\n",
		res.Category, res.Destination.Name, res.Destination.Action, res.Source)
}
