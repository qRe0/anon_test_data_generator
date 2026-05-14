package generator

import (
	"math/rand"
	"sync"
	"testing"
)

func TestReservoirIDPool_Empty(t *testing.T) {
	p := NewReservoirIDPool(5, 1)
	if p.Len() != 0 || p.Total() != 0 {
		t.Errorf("empty pool: Len=%d Total=%d", p.Len(), p.Total())
	}
	r := rand.New(rand.NewSource(0))
	if p.Sample(r) != nil {
		t.Error("Sample from empty pool should be nil")
	}
}

func TestReservoirIDPool_BelowCapacity(t *testing.T) {
	p := NewReservoirIDPool(10, 1)
	p.Append(int64(1), int64(2), int64(3))

	if p.Len() != 3 || p.Total() != 3 {
		t.Errorf("below capacity: Len=%d Total=%d", p.Len(), p.Total())
	}
}

func TestReservoirIDPool_AtCapacity(t *testing.T) {
	p := NewReservoirIDPool(5, 1)
	for i := int64(1); i <= 5; i++ {
		p.Append(i)
	}
	if p.Len() != 5 || p.Total() != 5 {
		t.Errorf("at capacity: Len=%d Total=%d", p.Len(), p.Total())
	}
}

func TestReservoirIDPool_AboveCapacity(t *testing.T) {
	p := NewReservoirIDPool(5, 1)
	for i := int64(1); i <= 100; i++ {
		p.Append(i)
	}
	if p.Len() > 5 {
		t.Errorf("reservoir exceeded capacity: Len=%d", p.Len())
	}
	if p.Total() != 100 {
		t.Errorf("Total=%d, want 100", p.Total())
	}
}

func TestReservoirIDPool_SampleReturnsInBounds(t *testing.T) {
	p := NewReservoirIDPool(5, 1)
	for i := int64(1); i <= 50; i++ {
		p.Append(i)
	}

	r := rand.New(rand.NewSource(42))
	seen := map[int64]bool{}
	for i := 0; i < 100; i++ {
		v := p.Sample(r).(int64)
		if v < 1 || v > 50 {
			t.Errorf("sample out of bounds: %d", v)
		}
		seen[v] = true
	}
	// At least some variety should be observed.
	if len(seen) < 2 {
		t.Errorf("not enough variety: %d unique values", len(seen))
	}
}

func TestReservoirIDPool_PreservesVariety(t *testing.T) {
	// With a large excess of IDs, the reservoir should still contain a spread.
	p := NewReservoirIDPool(200, 1)
	for i := int64(1); i <= 10000; i++ {
		p.Append(i)
	}
	if p.Len() < 100 {
		t.Errorf("expected at least 100 in reservoir, got %d", p.Len())
	}

	// Sample 1000 times and count unique values.
	r := rand.New(rand.NewSource(123))
	seen := map[int64]int{}
	for i := 0; i < 1000; i++ {
		v := p.Sample(r).(int64)
		seen[v]++
	}
	// With 200 reservoir slots and 10k total, should see 100+ unique values.
	if len(seen) < 50 {
		t.Errorf("too few unique samples: %d", len(seen))
	}
}

func TestReservoirIDPool_ConcurrentAppendAndSample(t *testing.T) {
	p := NewReservoirIDPool(100, 1)

	var wg sync.WaitGroup

	// 10 writers appending 1000 IDs each.
	for g := 0; g < 10; g++ {
		wg.Add(1)
		go func(seed int) {
			defer wg.Done()
			base := int64(seed * 1000)
			ids := make([]any, 1000)
			for i := range ids {
				ids[i] = base + int64(i)
			}
			p.Append(ids...)
		}(g)
	}

	// 5 readers sampling concurrently with writers.
	for g := 0; g < 5; g++ {
		wg.Add(1)
		go func(seed int) {
			defer wg.Done()
			r := rand.New(rand.NewSource(int64(seed)))
			for i := 0; i < 500; i++ {
				_ = p.Sample(r)
			}
		}(g)
	}

	wg.Wait()

	if p.Total() != 10000 {
		t.Errorf("Total=%d, want 10000", p.Total())
	}
	if p.Len() == 0 {
		t.Error("reservoir should not be empty after concurrent writes")
	}
}

func TestReservoirIDPool_DefaultCapacity(t *testing.T) {
	p := NewReservoirIDPool(0, 1)
	for i := int64(1); i <= 20000; i++ {
		p.Append(i)
	}
	if p.Len() != 10000 {
		t.Errorf("default capacity: Len=%d, want 10000", p.Len())
	}
}

func TestReservoirIDPool_Determinism(t *testing.T) {
	// Two pools with same data should produce same samples with same rand.
	p1 := NewReservoirIDPool(100, 1)
	p2 := NewReservoirIDPool(100, 1)
	for i := int64(1); i <= 200; i++ {
		p1.Append(i)
		p2.Append(i)
	}

	r1 := rand.New(rand.NewSource(42))
	r2 := rand.New(rand.NewSource(42))

	for i := 0; i < 50; i++ {
		s1 := p1.Sample(r1)
		s2 := p2.Sample(r2)
		if s1 != s2 {
			t.Fatalf("non-deterministic sample at call %d: %v != %v", i, s1, s2)
		}
	}
}

func TestReservoirIDPool_AppendBatch(t *testing.T) {
	p := NewReservoirIDPool(50, 1)
	batch := make([]any, 30)
	for i := range batch {
		batch[i] = int64(i + 1)
	}
	p.Append(batch...)

	if p.Len() != 30 {
		t.Errorf("batch append: Len=%d", p.Len())
	}
	if p.Total() != 30 {
		t.Errorf("batch append: Total=%d", p.Total())
	}
}
