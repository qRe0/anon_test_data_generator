package pii

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// mock analyzer
// ---------------------------------------------------------------------------

type mockAnalyzer struct {
	results []PresidioEntity
	err     error
}

func (m *mockAnalyzer) Analyze(_ context.Context, _, _ string) ([]PresidioEntity, error) {
	return m.results, m.err
}

// ---------------------------------------------------------------------------
// Tests: PresidioClient
// ---------------------------------------------------------------------------

func TestPresidioClient_Analyze_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/analyze" || r.Method != http.MethodPost {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		var req PresidioRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatal(err)
		}
		if req.Text != "John Doe" || req.Language != "en" {
			t.Errorf("req = %+v", req)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode([]PresidioEntity{
			{EntityType: "PERSON", Start: 0, End: 8, Score: 0.95},
		})
	}))
	defer srv.Close()

	c := NewPresidioClient(srv.URL)
	entities, err := c.Analyze(context.Background(), "John Doe", "en")
	if err != nil {
		t.Fatalf("Analyze() error: %v", err)
	}
	if len(entities) != 1 {
		t.Fatalf("expected 1 entity, got %d", len(entities))
	}
	if entities[0].EntityType != "PERSON" || entities[0].Score != 0.95 {
		t.Errorf("entity = %+v", entities[0])
	}
}

func TestPresidioClient_Analyze_HTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	c := NewPresidioClient(srv.URL)
	_, err := c.Analyze(context.Background(), "test", "en")
	if err == nil {
		t.Fatal("expected error for 500 status")
	}
	if !strings.Contains(err.Error(), "500") {
		t.Errorf("error = %v", err)
	}
}

func TestPresidioClient_Analyze_ConnectionRefused(t *testing.T) {
	c := NewPresidioClient("http://127.0.0.1:1")
	_, err := c.Analyze(context.Background(), "test", "en")
	if err == nil {
		t.Fatal("expected connection error")
	}
}

func TestPresidioClient_Analyze_InvalidJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("not-json"))
	}))
	defer srv.Close()

	c := NewPresidioClient(srv.URL)
	_, err := c.Analyze(context.Background(), "test", "en")
	if err == nil {
		t.Fatal("expected decode error")
	}
}

func TestPresidioClient_DefaultLanguage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req PresidioRequest
		json.NewDecoder(r.Body).Decode(&req)
		if req.Language != "en" {
			t.Errorf("expected default language 'en', got %q", req.Language)
		}
		json.NewEncoder(w).Encode([]PresidioEntity{})
	}))
	defer srv.Close()

	c := NewPresidioClient(srv.URL)
	_, err := c.Analyze(context.Background(), "test", "")
	if err != nil {
		t.Fatalf("Analyze() error: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Tests: Classifier
// ---------------------------------------------------------------------------

func TestClassifier_Classify_SingleColumn(t *testing.T) {
	analyzer := &mockAnalyzer{
		results: []PresidioEntity{
			{EntityType: "PERSON", Score: 0.95},
		},
	}
	classifier := NewClassifier(analyzer, "en")

	samples := []ColumnSample{
		{TableName: "public.users", ColumnName: "full_name", Values: []string{"John Doe", "Jane Smith", "Bob Wilson"}},
	}

	labels, err := classifier.Classify(context.Background(), samples)
	if err != nil {
		t.Fatalf("Classify() error: %v", err)
	}
	if len(labels) != 1 {
		t.Fatalf("expected 1 label, got %d", len(labels))
	}
	l := labels[0]
	if l.TableName != "public.users" || l.ColumnName != "full_name" {
		t.Errorf("column = %s.%s", l.TableName, l.ColumnName)
	}
	if l.Label.EntityType != "PERSON" {
		t.Errorf("entity = %q", l.Label.EntityType)
	}
	if l.Label.Generator != "person.name" {
		t.Errorf("generator = %q, want person.name", l.Label.Generator)
	}
}

func TestClassifier_Classify_MultipleColumns(t *testing.T) {
	callCount := 0
	analyzeFn := func(text string) []PresidioEntity {
		switch {
		case strings.Contains(text, "@"):
			return []PresidioEntity{{EntityType: "EMAIL_ADDRESS", Score: 0.9}}
		case strings.Contains(text, "+"):
			return []PresidioEntity{{EntityType: "PHONE_NUMBER", Score: 0.85}}
		default:
			return nil
		}
	}

	// Use httptest-based mock to have dynamic responses per call.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req PresidioRequest
		json.NewDecoder(r.Body).Decode(&req)
		callCount++
		entities := analyzeFn(req.Text)
		json.NewEncoder(w).Encode(entities)
	}))
	defer srv.Close()

	client := NewPresidioClient(srv.URL)
	c := NewClassifier(client, "en")

	samples := []ColumnSample{
		{TableName: "public.users", ColumnName: "email", Values: []string{"user1@test.com", "user2@test.com"}},
		{TableName: "public.users", ColumnName: "phone", Values: []string{"+1234567890", "+0987654321"}},
	}

	labels, err := c.Classify(context.Background(), samples)
	if err != nil {
		t.Fatalf("Classify() error: %v", err)
	}
	if len(labels) != 2 {
		t.Fatalf("expected 2 labels, got %d", len(labels))
	}
	if labels[0].Label.Generator != "internet.email" {
		t.Errorf("label[0] generator = %q", labels[0].Label.Generator)
	}
	if labels[1].Label.Generator != "phone.mobile" {
		t.Errorf("label[1] generator = %q", labels[1].Label.Generator)
	}
	if callCount != 4 {
		t.Errorf("expected 4 API calls, got %d", callCount)
	}
}

func TestClassifier_Classify_LowScoreIgnored(t *testing.T) {
	analyzer := &mockAnalyzer{
		results: []PresidioEntity{
			{EntityType: "PERSON", Score: 0.3}, // below default MinScore 0.5
		},
	}
	c := NewClassifier(analyzer, "en")

	samples := []ColumnSample{
		{TableName: "public.users", ColumnName: "full_name", Values: []string{"John"}},
	}

	labels, err := c.Classify(context.Background(), samples)
	if err != nil {
		t.Fatalf("Classify() error: %v", err)
	}
	if len(labels) != 0 {
		t.Errorf("expected 0 labels (low score), got %d", len(labels))
	}
}

func TestClassifier_Classify_NoMatchFraction(t *testing.T) {
	// Only return entities for name-like strings, leaving "42" unmatched.
	analyzer := &conditionalAnalyzer{
		fn: func(text string) []PresidioEntity {
			if text == "John" {
				return []PresidioEntity{{EntityType: "PERSON", Score: 0.9}}
			}
			return nil
		},
	}
	c := NewClassifier(analyzer, "en")
	c.MinMatchFraction = 0.8 // require 80%+ match

	values := make([]string, 10)
	for i := range values {
		if i < 3 {
			values[i] = "John" // only 30% are names, below 80% threshold
		} else {
			values[i] = "42"
		}
	}

	samples := []ColumnSample{
		{TableName: "t", ColumnName: "c", Values: values},
	}

	labels, err := c.Classify(context.Background(), samples)
	if err != nil {
		t.Fatalf("Classify() error: %v", err)
	}
	if len(labels) != 0 {
		t.Errorf("expected 0 labels (low match fraction), got %d", len(labels))
	}
}

// conditionalAnalyzer delegates to fn for dynamic responses.
type conditionalAnalyzer struct {
	fn func(text string) []PresidioEntity
}

func (ca *conditionalAnalyzer) Analyze(_ context.Context, text, _ string) ([]PresidioEntity, error) {
	return ca.fn(text), nil
}

func TestClassifier_Classify_EmptySamples(t *testing.T) {
	analyzer := &mockAnalyzer{} // not called
	c := NewClassifier(analyzer, "en")

	labels, err := c.Classify(context.Background(), nil)
	if err != nil {
		t.Fatalf("Classify() error: %v", err)
	}
	if len(labels) != 0 {
		t.Errorf("expected 0 labels, got %d", len(labels))
	}
}

func TestClassifier_Classify_EmptyValues(t *testing.T) {
	analyzer := &mockAnalyzer{} // not called
	c := NewClassifier(analyzer, "en")

	samples := []ColumnSample{
		{TableName: "t", ColumnName: "c", Values: []string{}},
	}

	labels, err := c.Classify(context.Background(), samples)
	if err != nil {
		t.Fatalf("Classify() error: %v", err)
	}
	if len(labels) != 0 {
		t.Errorf("expected 0 labels, got %d", len(labels))
	}
}

func TestClassifier_Classify_AllEmptyStrings(t *testing.T) {
	analyzer := &mockAnalyzer{} // not called
	c := NewClassifier(analyzer, "en")

	samples := []ColumnSample{
		{TableName: "t", ColumnName: "c", Values: []string{"", "", ""}},
	}

	labels, err := c.Classify(context.Background(), samples)
	if err != nil {
		t.Fatalf("Classify() error: %v", err)
	}
	if len(labels) != 0 {
		t.Errorf("expected 0 labels, got %d", len(labels))
	}
}

func TestClassifier_DefaultLanguage(t *testing.T) {
	c := NewClassifier(nil, "")
	if c.Language != "en" {
		t.Errorf("Language = %q", c.Language)
	}
	if c.MinScore != 0.5 {
		t.Errorf("MinScore = %f", c.MinScore)
	}
	if c.MinMatchFraction != 0.5 {
		t.Errorf("MinMatchFraction = %f", c.MinMatchFraction)
	}
}

// ---------------------------------------------------------------------------
// Tests: EntityMapping
// ---------------------------------------------------------------------------

func TestEntityMapping_KnownTypes(t *testing.T) {
	tests := []struct {
		entity    string
		generator string
	}{
		{"PERSON", "person.name"},
		{"EMAIL_ADDRESS", "internet.email"},
		{"PHONE_NUMBER", "phone.mobile"},
		{"CREDIT_CARD", "finance.credit_card"},
		{"DATE_TIME", "time.timestamp"},
	}

	for _, tt := range tests {
		gen, ok := EntityMapping[tt.entity]
		if !ok {
			t.Errorf("missing mapping for %q", tt.entity)
			continue
		}
		if gen != tt.generator {
			t.Errorf("EntityMapping[%q] = %q, want %q", tt.entity, gen, tt.generator)
		}
	}
}

func TestEntityMapping_UnknownType(t *testing.T) {
	if _, ok := EntityMapping["URL"]; ok {
		t.Error("URL should not be mapped (no direct generator)")
	}
	if _, ok := EntityMapping["IP_ADDRESS"]; ok {
		t.Error("IP_ADDRESS should not be mapped")
	}
}

func TestClassifier_UnmappedEntityIgnored(t *testing.T) {
	analyzer := &mockAnalyzer{
		results: []PresidioEntity{
			{EntityType: "URL", Score: 0.95}, // not in EntityMapping
		},
	}
	c := NewClassifier(analyzer, "en")

	samples := []ColumnSample{
		{TableName: "t", ColumnName: "link", Values: []string{"https://example.com"}},
	}

	labels, err := c.Classify(context.Background(), samples)
	if err != nil {
		t.Fatalf("Classify() error: %v", err)
	}
	if len(labels) != 0 {
		t.Errorf("expected 0 labels (unmapped entity), got %d", len(labels))
	}
}
