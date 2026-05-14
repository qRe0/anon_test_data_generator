package generator

import (
	"math/rand"
	"sync"
)

const defaultReservoirSize = 10000

// ReservoirIDPool implements IDPool with reservoir sampling to bound memory.
// When the number of appended IDs exceeds the reservoir capacity,
// a random replacement strategy ensures uniform sampling from the full stream.
// The pool is seeded for deterministic behavior.
type ReservoirIDPool struct {
	mu        sync.RWMutex
	reservoir []any
	count     int
	capacity  int
	rnd       *rand.Rand // internal random source for reservoir decisions
}

// NewReservoirIDPool creates a pool with the given capacity and seed.
// If capacity <= 0, the default (10,000) is used.
func NewReservoirIDPool(capacity int, seed int64) *ReservoirIDPool {
	if capacity <= 0 {
		capacity = defaultReservoirSize
	}
	return &ReservoirIDPool{
		reservoir: make([]any, 0, capacity),
		capacity:  capacity,
		rnd:       rand.New(rand.NewSource(seed)),
	}
}

// Append adds IDs to the pool using reservoir sampling.
func (p *ReservoirIDPool) Append(ids ...any) {
	p.mu.Lock()
	defer p.mu.Unlock()

	for _, id := range ids {
		p.count++
		if len(p.reservoir) < p.capacity {
			p.reservoir = append(p.reservoir, id)
		} else {
			j := p.rnd.Intn(p.count)
			if j < p.capacity {
				p.reservoir[j] = id
			}
		}
	}
}

// Sample returns a uniformly random element from the reservoir.
func (p *ReservoirIDPool) Sample(r *rand.Rand) any {
	p.mu.RLock()
	defer p.mu.RUnlock()

	if len(p.reservoir) == 0 {
		return nil
	}
	return p.reservoir[r.Intn(len(p.reservoir))]
}

// Len returns the number of elements currently in the reservoir.
func (p *ReservoirIDPool) Len() int {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return len(p.reservoir)
}

// Total returns the total number of IDs appended (including those not in the reservoir).
func (p *ReservoirIDPool) Total() int {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.count
}
