package generator

import (
	"math"
	"math/rand"
)

// ValueGenerator produces a synthetic value for a single column.
type ValueGenerator interface {
	Generate(params Params, r *rand.Rand) any
}

// Transformer modifies a generated value (e.g., masking, nulling).
type Transformer interface {
	Apply(value any, params Params) any
}

// IDPool stores primary keys of a parent table and provides random samples.
type IDPool interface {
	Append(ids ...any)
	Sample(r *rand.Rand) any
	Len() int
}

// Registry holds named generators and transformers.
type Registry struct {
	generators   map[string]ValueGenerator
	transformers map[string]Transformer
}

// NewRegistry creates an empty Registry.
func NewRegistry() *Registry {
	return &Registry{
		generators:   make(map[string]ValueGenerator),
		transformers: make(map[string]Transformer),
	}
}

// Register adds a generator under the given name.
func (r *Registry) Register(name string, g ValueGenerator) {
	r.generators[name] = g
}

// Get returns the generator registered under name.
func (r *Registry) Get(name string) (ValueGenerator, bool) {
	g, ok := r.generators[name]
	return g, ok
}

// IsRegistered implements config.GeneratorRegistry.
func (r *Registry) IsRegistered(name string) bool {
	_, ok := r.generators[name]
	return ok
}

// RegisterTransformer adds a transformer under the given name.
func (r *Registry) RegisterTransformer(name string, t Transformer) {
	r.transformers[name] = t
}

// GetTransformer returns the transformer registered under name.
func (r *Registry) GetTransformer(name string) (Transformer, bool) {
	t, ok := r.transformers[name]
	return t, ok
}

// Params wraps a column's generator parameters with typed accessors.
type Params map[string]any

// String returns the string value for key, or def if missing/wrong type.
func (p Params) String(key, def string) string {
	if p == nil {
		return def
	}
	v, ok := p[key]
	if !ok {
		return def
	}
	s, ok := v.(string)
	if !ok {
		return def
	}
	return s
}

// Int returns the int value for key, or def if missing/wrong type.
func (p Params) Int(key string, def int) int {
	if p == nil {
		return def
	}
	v, ok := p[key]
	if !ok {
		return def
	}
	switch n := v.(type) {
	case int:
		return n
	case int64:
		return int(n)
	case float64:
		return int(math.Round(n))
	}
	return def
}

// Float returns the float64 value for key, or def if missing/wrong type.
func (p Params) Float(key string, def float64) float64 {
	if p == nil {
		return def
	}
	v, ok := p[key]
	if !ok {
		return def
	}
	switch n := v.(type) {
	case float64:
		return n
	case int:
		return float64(n)
	case int64:
		return float64(n)
	}
	return def
}

// Bool returns the bool value for key, or def if missing/wrong type.
func (p Params) Bool(key string, def bool) bool {
	if p == nil {
		return def
	}
	v, ok := p[key]
	if !ok {
		return def
	}
	b, ok := v.(bool)
	if !ok {
		return def
	}
	return b
}

// StringSlice returns a []string value for key.
func (p Params) StringSlice(key string) []string {
	if p == nil {
		return nil
	}
	v, ok := p[key]
	if !ok {
		return nil
	}
	raw, ok := v.([]any)
	if !ok {
		return nil
	}
	out := make([]string, len(raw))
	for i, e := range raw {
		s, _ := e.(string)
		out[i] = s
	}
	return out
}

// FloatSlice returns a []float64 value for key.
func (p Params) FloatSlice(key string) []float64 {
	if p == nil {
		return nil
	}
	v, ok := p[key]
	if !ok {
		return nil
	}
	raw, ok := v.([]any)
	if !ok {
		return nil
	}
	out := make([]float64, len(raw))
	for i, e := range raw {
		switch n := e.(type) {
		case float64:
			out[i] = n
		case int:
			out[i] = float64(n)
		}
	}
	return out
}

// DefaultRegistry returns a Registry pre-populated with all built-in generators
// and transformers for the given locale.
func DefaultRegistry(locale string) *Registry {
	r := NewRegistry()
	registerAllGenerators(r, locale)
	registerAllTransformers(r)
	return r
}

// registerAllGenerators registers every built-in generator.
func registerAllGenerators(r *Registry, locale string) {
	r.Register("autoincrement", &autoIncrementGen{})
	r.Register("person.name", newPersonNameGen(locale))
	r.Register("internet.email", newEmailGen(locale))
	r.Register("phone.mobile", newPhoneGen())
	r.Register("time.date", &dateGen{})
	r.Register("time.timestamp", &timestampGen{})
	r.Register("finance.amount", &amountGen{})
	r.Register("finance.credit_card", newCreditCardGen(locale))
	r.Register("uuid", &uuidGen{})
	r.Register("collection.random_choice", &randomChoiceGen{})
	r.Register("static.value", &staticValueGen{})
	r.Register("relation.foreign_key", &foreignKeyGen{})
}

// registerAllTransformers registers every built-in transformer.
func registerAllTransformers(r *Registry) {
	r.RegisterTransformer("masking.partial", &partialMaskTransformer{})
	r.RegisterTransformer("nulling", &nullingTransformer{})
}


