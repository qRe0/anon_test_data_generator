package generator

import (
	"math/rand"
	"strings"
	"sync"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// Registry
// ---------------------------------------------------------------------------

func TestRegistry_RegisterAndGet(t *testing.T) {
	r := NewRegistry()
	r.Register("test.gen", &staticValueGen{})

	g, ok := r.Get("test.gen")
	if !ok {
		t.Fatal("expected generator to be registered")
	}
	if g == nil {
		t.Error("generator is nil")
	}
}

func TestRegistry_IsRegistered(t *testing.T) {
	r := NewRegistry()
	r.Register("known", &uuidGen{})

	if !r.IsRegistered("known") {
		t.Error("IsRegistered(known) should be true")
	}
	if r.IsRegistered("unknown") {
		t.Error("IsRegistered(unknown) should be false")
	}
}

func TestRegistry_GetUnknown(t *testing.T) {
	r := NewRegistry()
	_, ok := r.Get("missing")
	if ok {
		t.Error("expected missing generator")
	}
}

func TestRegistry_RegisterTransformer(t *testing.T) {
	r := NewRegistry()
	r.RegisterTransformer("nulling", &nullingTransformer{})

	tr, ok := r.GetTransformer("nulling")
	if !ok {
		t.Fatal("expected transformer to be registered")
	}
	if tr == nil {
		t.Error("transformer is nil")
	}
}

func TestRegistry_GetTransformerUnknown(t *testing.T) {
	r := NewRegistry()
	_, ok := r.GetTransformer("missing")
	if ok {
		t.Error("expected missing transformer")
	}
}

func TestDefaultRegistry_HasAllGenerators(t *testing.T) {
	r := DefaultRegistry("en")
	expected := []string{
		"autoincrement", "person.name", "internet.email", "phone.mobile",
		"time.date", "time.timestamp", "finance.amount", "finance.credit_card",
		"uuid", "collection.random_choice", "static.value", "relation.foreign_key",
	}
	for _, name := range expected {
		if !r.IsRegistered(name) {
			t.Errorf("generator %q not registered", name)
		}
	}
}

func TestDefaultRegistry_HasAllTransformers(t *testing.T) {
	r := DefaultRegistry("en")
	for _, name := range []string{"masking.partial", "nulling"} {
		if _, ok := r.GetTransformer(name); !ok {
			t.Errorf("transformer %q not registered", name)
		}
	}
}

// ---------------------------------------------------------------------------
// Params helpers
// ---------------------------------------------------------------------------

func TestParams_String(t *testing.T) {
	p := Params{"key": "hello"}
	if p.String("key", "def") != "hello" {
		t.Error("String failed")
	}
	if p.String("missing", "def") != "def" {
		t.Error("String default failed")
	}
	var nilP Params
	if nilP.String("any", "d") != "d" {
		t.Error("nil Params default failed")
	}
}

func TestParams_Int(t *testing.T) {
	p := Params{"n": 42, "f": 3.9}
	if p.Int("n", 0) != 42 {
		t.Error("Int failed")
	}
	if p.Int("f", 0) != 4 {
		t.Error("Int from float64 failed")
	}
	if p.Int("missing", 99) != 99 {
		t.Error("Int default failed")
	}
}

func TestParams_Float(t *testing.T) {
	p := Params{"x": 3.5, "i": 2}
	if p.Float("x", 0) != 3.5 {
		t.Error("Float failed")
	}
	if p.Float("i", 0) != 2.0 {
		t.Error("Float from int failed")
	}
	if p.Float("missing", 1.1) != 1.1 {
		t.Error("Float default failed")
	}
}

func TestParams_Bool(t *testing.T) {
	p := Params{"t": true}
	if !p.Bool("t", false) {
		t.Error("Bool true failed")
	}
	if p.Bool("missing", true) != true {
		t.Error("Bool default failed")
	}
}

func TestParams_StringSlice(t *testing.T) {
	raw := []any{"a", "b", "c"}
	p := Params{"items": raw}
	got := p.StringSlice("items")
	if len(got) != 3 || got[0] != "a" || got[2] != "c" {
		t.Errorf("StringSlice = %v", got)
	}
	if p.StringSlice("missing") != nil {
		t.Error("StringSlice missing should be nil")
	}
}

func TestParams_FloatSlice(t *testing.T) {
	raw := []any{1.0, 2.5, 3}
	p := Params{"w": raw}
	got := p.FloatSlice("w")
	if len(got) != 3 || got[0] != 1.0 || got[1] != 2.5 {
		t.Errorf("FloatSlice = %v", got)
	}
}

// ---------------------------------------------------------------------------
// Generators
// ---------------------------------------------------------------------------

func TestAutoIncrement(t *testing.T) {
	gen := &autoIncrementGen{}
	p := Params{"start": 1, "step": 1}
	r := rand.New(rand.NewSource(0))

	v1 := gen.Generate(p, r).(int64)
	v2 := gen.Generate(p, r).(int64)
	v3 := gen.Generate(p, r).(int64)

	if v1 != 1 || v2 != 2 || v3 != 3 {
		t.Errorf("autoincrement = %d %d %d, want 1 2 3", v1, v2, v3)
	}
}

func TestAutoIncrement_Concurrent(t *testing.T) {
	gen := &autoIncrementGen{}
	p := Params{"start": 100}
	var wg sync.WaitGroup
	const n = 100
	results := make([]int64, n)

	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			r := rand.New(rand.NewSource(int64(idx)))
			results[idx] = gen.Generate(p, r).(int64)
		}(i)
	}
	wg.Wait()

	seen := map[int64]bool{}
	for _, v := range results {
		if seen[v] {
			t.Errorf("duplicate autoincrement value %d", v)
		}
		seen[v] = true
	}
}

func TestPersonName(t *testing.T) {
	gen := newPersonNameGen("en")
	r := rand.New(rand.NewSource(42))
	name := gen.Generate(nil, r).(string)
	if name == "" || !strings.Contains(name, " ") {
		t.Errorf("person name = %q", name)
	}
}

func TestPersonName_Determinism(t *testing.T) {
	gen := newPersonNameGen("en")
	r1 := rand.New(rand.NewSource(1))
	r2 := rand.New(rand.NewSource(1))
	if gen.Generate(nil, r1) != gen.Generate(nil, r2) {
		t.Error("person name not deterministic with same seed")
	}
}

func TestEmail(t *testing.T) {
	gen := newEmailGen("en")
	r := rand.New(rand.NewSource(42))

	email := gen.Generate(nil, r).(string)
	if !strings.Contains(email, "@") {
		t.Errorf("email = %q", email)
	}
}

func TestEmail_WithDomain(t *testing.T) {
	gen := newEmailGen("en")
	r := rand.New(rand.NewSource(42))

	email := gen.Generate(Params{"domain": "example-test.com"}, r).(string)
	if !strings.HasSuffix(email, "@example-test.com") {
		t.Errorf("email with domain = %q", email)
	}
}

func TestPhoneMobile(t *testing.T) {
	gen := &phoneGen{}
	r := rand.New(rand.NewSource(42))

	phone := gen.Generate(Params{"format": "+1 (###) ###-##-##"}, r).(string)
	if len(phone) != len("+1 (###) ###-##-##") {
		t.Errorf("phone = %q", phone)
	}
	// All # should be replaced with digits.
	for _, c := range phone {
		if c == '#' {
			t.Errorf("unreplaced # in %q", phone)
		}
	}
}

func TestPhoneMobile_DefaultFormat(t *testing.T) {
	gen := &phoneGen{}
	r := rand.New(rand.NewSource(42))
	phone := gen.Generate(nil, r).(string)
	if len(phone) != 10 {
		t.Errorf("default phone = %q (len=%d)", phone, len(phone))
	}
}

func TestDate(t *testing.T) {
	gen := &dateGen{}
	r := rand.New(rand.NewSource(42))
	date := gen.Generate(Params{"min_age": 20, "max_age": 30}, r).(string)

	parsed, err := time.Parse("2006-01-02", date)
	if err != nil {
		t.Fatalf("date parse error: %v", err)
	}
	now := time.Now()
	age := now.Year() - parsed.Year()
	if age < 20 || age > 30 {
		t.Errorf("age = %d (date = %s)", age, date)
	}
}

func TestDate_Determinism(t *testing.T) {
	gen := &dateGen{}
	p := Params{"min_age": 25, "max_age": 40}
	r1 := rand.New(rand.NewSource(99))
	r2 := rand.New(rand.NewSource(99))
	if gen.Generate(p, r1) != gen.Generate(p, r2) {
		t.Error("date not deterministic")
	}
}

func TestTimestamp(t *testing.T) {
	gen := &timestampGen{}
	p := Params{
		"min": "2023-01-01 00:00:00",
		"max": "2023-12-31 23:59:59",
	}
	r := rand.New(rand.NewSource(42))
	ts := gen.Generate(p, r).(string)

	parsed, err := time.Parse(tsLayout, ts)
	if err != nil {
		t.Fatalf("timestamp parse error: %v", err)
	}
	if parsed.Year() != 2023 {
		t.Errorf("timestamp year = %d", parsed.Year())
	}
}

func TestTimestamp_WithNow(t *testing.T) {
	gen := &timestampGen{}
	p := Params{
		"min": "2024-01-01 00:00:00",
		"max": "now",
	}
	r := rand.New(rand.NewSource(42))
	ts := gen.Generate(p, r).(string)

	parsed, err := time.Parse(tsLayout, ts)
	if err != nil {
		t.Fatalf("timestamp parse error: %v", err)
	}
	if parsed.Before(time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("timestamp before min: %s", ts)
	}
}

func TestAmount(t *testing.T) {
	gen := &amountGen{}
	p := Params{"min": 10.0, "max": 20.0, "decimals": 2}
	r := rand.New(rand.NewSource(42))

	v := gen.Generate(p, r).(float64)
	if v < 10.0 || v > 20.0 {
		t.Errorf("amount = %f", v)
	}
}

func TestAmount_Determinism(t *testing.T) {
	gen := &amountGen{}
	p := Params{"min": 0.0, "max": 1000.0, "decimals": 2}
	r1 := rand.New(rand.NewSource(7))
	r2 := rand.New(rand.NewSource(7))
	if gen.Generate(p, r1) != gen.Generate(p, r2) {
		t.Error("amount not deterministic")
	}
}

func TestCreditCard(t *testing.T) {
	gen := newCreditCardGen("en")
	r := rand.New(rand.NewSource(42))

	num := gen.Generate(nil, r).(string)
	if len(num) < 13 || len(num) > 19 {
		t.Errorf("credit card = %q (len=%d)", num, len(num))
	}
	if !luhnValid(num) {
		t.Errorf("credit card %q failed Luhn check", num)
	}
}

func TestCreditCard_Determinism(t *testing.T) {
	gen := newCreditCardGen("en")
	r1 := rand.New(rand.NewSource(42))
	r2 := rand.New(rand.NewSource(42))
	if gen.Generate(nil, r1) != gen.Generate(nil, r2) {
		t.Error("credit card not deterministic")
	}
}

func TestUUID(t *testing.T) {
	gen := &uuidGen{}
	r := rand.New(rand.NewSource(42))
	id := gen.Generate(nil, r).(string)
	if len(id) != 36 || strings.Count(id, "-") != 4 {
		t.Errorf("uuid = %q", id)
	}
}

func TestRandomChoice_Uniform(t *testing.T) {
	gen := &randomChoiceGen{}
	p := Params{"choices": []any{"a", "b", "c"}}
	r := rand.New(rand.NewSource(42))

	seen := map[string]int{}
	for i := 0; i < 300; i++ {
		v := gen.Generate(p, r).(string)
		seen[v]++
	}
	if seen["a"] == 0 || seen["b"] == 0 || seen["c"] == 0 {
		t.Errorf("uniform choice distribution: %v", seen)
	}
}

func TestRandomChoice_Weighted(t *testing.T) {
	gen := &randomChoiceGen{}
	p := Params{
		"choices": []any{"rare", "common"},
		"weights": []any{1.0, 99.0},
	}
	r := rand.New(rand.NewSource(42))

	rareCount := 0
	for i := 0; i < 1000; i++ {
		v := gen.Generate(p, r).(string)
		if v == "rare" {
			rareCount++
		}
	}
	if rareCount == 0 || rareCount > 100 {
		t.Errorf("weighted rare count = %d (expected 1%% of 1000)", rareCount)
	}
}

func TestRandomChoice_Empty(t *testing.T) {
	gen := &randomChoiceGen{}
	r := rand.New(rand.NewSource(42))
	v := gen.Generate(nil, r)
	if v != nil {
		t.Errorf("expected nil for empty choices, got %v", v)
	}
}

func TestStaticValue(t *testing.T) {
	gen := &staticValueGen{}
	v := gen.Generate(Params{"value": "hello world"}, nil)
	if v != "hello world" {
		t.Errorf("static value = %v", v)
	}
}

func TestStaticValue_Int(t *testing.T) {
	gen := &staticValueGen{}
	v := gen.Generate(Params{"value": 42}, nil)
	if v != 42 {
		t.Errorf("static int = %v", v)
	}
}

func TestForeignKey_NoProvider(t *testing.T) {
	gen := &foreignKeyGen{}
	r := rand.New(rand.NewSource(42))
	v := gen.Generate(Params{"table": "users"}, r)
	if v != nil {
		t.Errorf("expected nil from foreign_key without pool, got %v", v)
	}
}

func TestForeignKey_WithProvider(t *testing.T) {
	mockPool := &stubPool{values: []any{int64(1), int64(2), int64(3)}}
	provider := func(name string) IDPool {
		if name == "users" {
			return mockPool
		}
		return nil
	}

	gen := &foreignKeyGen{}
	p := Params{
		"table":           "users",
		"column":          "id",
		"__pool_provider": PoolProvider(provider),
	}
	r := rand.New(rand.NewSource(42))
	v := gen.Generate(p, r)
	if v == nil {
		t.Fatal("expected non-nil FK value")
	}
}

type stubPool struct {
	values []any
}

func (p *stubPool) Append(ids ...any)    { p.values = append(p.values, ids...) }
func (p *stubPool) Sample(r *rand.Rand) any {
	if len(p.values) == 0 {
		return nil
	}
	return p.values[r.Intn(len(p.values))]
}
func (p *stubPool) Len() int { return len(p.values) }

// ---------------------------------------------------------------------------
// Transformers
// ---------------------------------------------------------------------------

func TestPartialMask(t *testing.T) {
	tr := &partialMaskTransformer{}
	p := Params{"pattern": "+1 (***) ***-**-##"}
	result := tr.Apply("+1 (555) 123-45-67", p)
	expected := "+1 (***) ***-**-67"
	if result != expected {
		t.Errorf("partial mask = %q, want %q", result, expected)
	}
}

func TestPartialMask_Default(t *testing.T) {
	tr := &partialMaskTransformer{}
	result := tr.Apply("hello", Params{})
	if result != "hello" {
		t.Errorf("partial mask default = %q", result)
	}
}

func TestPartialMask_NonString(t *testing.T) {
	tr := &partialMaskTransformer{}
	result := tr.Apply(42, Params{"pattern": "**"})
	if result != 42 {
		t.Errorf("partial mask non-string = %v", result)
	}
}

func TestNulling(t *testing.T) {
	tr := &nullingTransformer{}
	result := tr.Apply("anything", nil)
	if result != nil {
		t.Errorf("nulling = %v", result)
	}
}

// ---------------------------------------------------------------------------
// Determinism across generators
// ---------------------------------------------------------------------------

func TestDeterminism_SameSeedSameOutput(t *testing.T) {
	gen := DefaultRegistry("en")

	tests := []struct {
		name string
		gen  string
		p    Params
	}{
		{"person.name", "person.name", nil},
		{"email", "internet.email", Params{"domain": "test.com"}},
		{"phone", "phone.mobile", Params{"format": "+1 (###) ###-####"}},
		{"date", "time.date", Params{"min_age": 25, "max_age": 35}},
		{"timestamp", "time.timestamp", Params{"min": "2023-01-01 00:00:00", "max": "now"}},
		{"amount", "finance.amount", Params{"min": 10.0, "max": 100.0, "decimals": 2}},
		{"uuid", "uuid", nil},
		{"random_choice", "collection.random_choice", Params{"choices": []any{"a", "b", "c"}}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g, _ := gen.Get(tt.gen)
			r1 := rand.New(rand.NewSource(12345))
			r2 := rand.New(rand.NewSource(12345))
			v1 := g.Generate(tt.p, r1)
			v2 := g.Generate(tt.p, r2)
			if v1 != v2 {
				t.Errorf("non-deterministic: %v != %v", v1, v2)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Luhn check helper
// ---------------------------------------------------------------------------

func luhnValid(number string) bool {
	sum := 0
	double := false
	for i := len(number) - 1; i >= 0; i-- {
		d := int(number[i] - '0')
		if d < 0 || d > 9 {
			continue
		}
		if double {
			d *= 2
			if d > 9 {
				d -= 9
			}
		}
		sum += d
		double = !double
	}
	return sum%10 == 0
}
