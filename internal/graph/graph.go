package graph

import (
	"fmt"
	"strings"

	"github.com/anomalyco/anon_test_data_generator/internal/schema"
)

// Edge represents a dependency: From must be generated before To.
// From is the referenced (parent) table; To is the table with the foreign key.
type Edge struct {
	From string
	To   string
}

// Graph is a directed acyclic graph of table dependencies.
type Graph struct {
	Nodes []string
	Edges []Edge
}

// ExecutionPlan is the result of topological sorting.
// Tables within the same level have no dependencies on each other
// and can be generated in parallel.
type ExecutionPlan struct {
	Levels [][]string
	Order  []string // flat topological order
}

// CycleError is returned when the graph contains circular FK dependencies.
type CycleError struct {
	Remaining []string // tables stuck in the cycle
}

func (e *CycleError) Error() string {
	return fmt.Sprintf("circular dependency detected among tables: [%s]",
		strings.Join(e.Remaining, ", "))
}

// BuildExecutionPlan constructs a dependency graph from schema metadata,
// runs Kahn's algorithm, and returns an ExecutionPlan with level grouping.
func BuildExecutionPlan(tables map[string]*schema.TableMeta) (*ExecutionPlan, error) {
	g := buildGraph(tables)
	return kahnSort(g)
}

// buildGraph extracts edges from schema FK references.
func buildGraph(tables map[string]*schema.TableMeta) *Graph {
	g := &Graph{}
	seen := map[string]bool{}

	for name, tm := range tables {
		if !seen[name] {
			g.Nodes = append(g.Nodes, name)
			seen[name] = true
		}
		for _, col := range tm.Columns {
			if col.ForeignKey == nil {
				continue
			}
			ref := col.ForeignKey.RefQualifiedName()
			if _, ok := tables[ref]; !ok {
				continue // skip FKs to tables not in the configured set
			}
			if !seen[ref] {
				g.Nodes = append(g.Nodes, ref)
				seen[ref] = true
			}
			g.Edges = append(g.Edges, Edge{From: ref, To: name})
		}
	}
	return g
}

// kahnSort performs Kahn's topological sort with level grouping.
func kahnSort(g *Graph) (*ExecutionPlan, error) {
	if len(g.Nodes) == 0 {
		return &ExecutionPlan{}, nil
	}

	// Build indegree map and adjacency list.
	indegree := make(map[string]int)
	adj := make(map[string][]string)
	for _, n := range g.Nodes {
		indegree[n] = 0
		adj[n] = nil
	}
	for _, e := range g.Edges {
		indegree[e.To]++
		adj[e.From] = append(adj[e.From], e.To)
	}

	// Collect starting nodes (indegree 0).
	var current []string
	for _, n := range g.Nodes {
		if indegree[n] == 0 {
			current = append(current, n)
		}
	}

	plan := &ExecutionPlan{}
	for len(current) > 0 {
		plan.Levels = append(plan.Levels, current)
		plan.Order = append(plan.Order, current...)

		var next []string
		for _, n := range current {
			for _, child := range adj[n] {
				indegree[child]--
				if indegree[child] == 0 {
					next = append(next, child)
				}
			}
		}
		current = next
	}

	if len(plan.Order) != len(g.Nodes) {
		// Collect remaining nodes for error reporting.
		var remaining []string
		for _, n := range g.Nodes {
			if indegree[n] > 0 {
				remaining = append(remaining, n)
			}
		}
		return nil, &CycleError{Remaining: remaining}
	}

	return plan, nil
}

// IsIndependent returns true if all tables in the group have no dependency edges
// between them and share the same level.
func (p *ExecutionPlan) Level(i int) []string {
	if i < 0 || i >= len(p.Levels) {
		return nil
	}
	return p.Levels[i]
}

// TotalTables returns the total number of tables in the plan.
func (p *ExecutionPlan) TotalTables() int {
	return len(p.Order)
}
