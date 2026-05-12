package pii

import (
	"context"
	"fmt"
)

// ColumnLabel combines a fully qualified column reference with its detected PII label.
type ColumnLabel struct {
	TableName  string
	ColumnName string
	Label      PiiLabel
}

// Classifier analyzes column samples and assigns PII labels.
type Classifier struct {
	Analyzer Analyzer
	Language string
	// MinScore is the minimum confidence to consider a detection valid (0.0–1.0).
	MinScore float64
	// MinMatchFraction is the minimum fraction of sampled rows that must match
	// the same entity type for the column to be labeled as PII (0.0–1.0).
	MinMatchFraction float64
}

// NewClassifier creates a Classifier with sensible defaults.
func NewClassifier(a Analyzer, lang string) *Classifier {
	if lang == "" {
		lang = "en"
	}
	return &Classifier{
		Analyzer:         a,
		Language:         lang,
		MinScore:         0.5,
		MinMatchFraction: 0.5,
	}
}

// Classify processes column samples and returns PII labels for columns that
// exceed the confidence thresholds.
func (c *Classifier) Classify(ctx context.Context, samples []ColumnSample) ([]ColumnLabel, error) {
	var results []ColumnLabel

	for _, cs := range samples {
		label, err := c.classifyColumn(ctx, cs)
		if err != nil {
			return nil, fmt.Errorf("classify %s.%s: %w", cs.TableName, cs.ColumnName, err)
		}
		if label != nil {
			results = append(results, ColumnLabel{
				TableName:  cs.TableName,
				ColumnName: cs.ColumnName,
				Label:      *label,
			})
		}
	}

	return results, nil
}

func (c *Classifier) classifyColumn(ctx context.Context, cs ColumnSample) (*PiiLabel, error) {
	if len(cs.Values) == 0 {
		return nil, nil
	}

	counts := map[string]int{}
	totalScore := map[string]float64{}
	total := 0

	for _, value := range cs.Values {
		if value == "" {
			continue
		}
		entities, err := c.Analyzer.Analyze(ctx, value, c.Language)
		if err != nil {
			return nil, err
		}
		for _, e := range entities {
			if e.Score < c.MinScore {
				continue
			}
			counts[e.EntityType]++
			totalScore[e.EntityType] += e.Score
			total++
		}
	}

	if total == 0 {
		return nil, nil
	}

	// Find the dominant entity type.
	var dominant string
	var dominantCount int
	for et, cnt := range counts {
		if cnt > dominantCount {
			dominant = et
			dominantCount = cnt
		}
	}

	fraction := float64(dominantCount) / float64(len(cs.Values))
	if fraction < c.MinMatchFraction {
		return nil, nil
	}

	avgScore := totalScore[dominant] / float64(dominantCount)

	gen, ok := EntityMapping[dominant]
	if !ok || gen == "" {
		return nil, nil // no mapped generator, skip labeling
	}

	return &PiiLabel{
		EntityType: dominant,
		Score:      avgScore,
		Generator:  gen,
	}, nil
}
