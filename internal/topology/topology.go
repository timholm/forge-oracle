// Package topology implements TopologyOptimizer: tracks build outcomes and
// recommends model selection per pipeline phase based on historical performance.
//
// Techniques applied:
//   - ABSTRAL multi-agent topology optimization (arXiv:2603.22791): treat agent
//     wiring as an evolvable document refined by contrastive trace analysis.
//   - Minibal balanced calibration (arXiv:2603.23059): calibrate model strength
//     per phase rather than always using the most powerful model.
package topology

import (
	"fmt"
	"math"
	"sort"
	"sync"
	"time"

	"github.com/timholm/forge-oracle/internal/types"
)

// phaseModels lists the available models for each phase.
var defaultModels = []string{"opus", "sonnet", "haiku"}

// TopologyOptimizer tracks build outcomes and generates model routing
// recommendations based on contrastive analysis of successful vs failed runs.
// In production, this persists to Postgres; here we provide an in-memory store
// with a Store interface for pluggable backends.
type TopologyOptimizer struct {
	mu       sync.RWMutex
	outcomes []types.BuildOutcome
	store    Store
}

// Store is the persistence interface for build outcome history.
// Implement this with Postgres for production use.
type Store interface {
	SaveOutcome(outcome types.BuildOutcome) error
	LoadOutcomes(phase string, limit int) ([]types.BuildOutcome, error)
	LoadAllOutcomes(limit int) ([]types.BuildOutcome, error)
}

// MemoryStore is an in-memory Store implementation for testing and standalone use.
type MemoryStore struct {
	mu       sync.RWMutex
	outcomes []types.BuildOutcome
}

// NewMemoryStore creates a new in-memory store.
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{}
}

// SaveOutcome stores an outcome in memory.
func (s *MemoryStore) SaveOutcome(outcome types.BuildOutcome) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.outcomes = append(s.outcomes, outcome)
	return nil
}

// LoadOutcomes returns outcomes for a specific phase.
func (s *MemoryStore) LoadOutcomes(phase string, limit int) ([]types.BuildOutcome, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var filtered []types.BuildOutcome
	for _, o := range s.outcomes {
		if o.Phase == phase {
			filtered = append(filtered, o)
		}
	}

	// Return most recent
	if limit > 0 && len(filtered) > limit {
		filtered = filtered[len(filtered)-limit:]
	}
	return filtered, nil
}

// LoadAllOutcomes returns all outcomes.
func (s *MemoryStore) LoadAllOutcomes(limit int) ([]types.BuildOutcome, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	result := make([]types.BuildOutcome, len(s.outcomes))
	copy(result, s.outcomes)
	if limit > 0 && len(result) > limit {
		result = result[len(result)-limit:]
	}
	return result, nil
}

// New creates a TopologyOptimizer with the given store.
func New(store Store) *TopologyOptimizer {
	return &TopologyOptimizer{
		store: store,
	}
}

// NewInMemory creates a TopologyOptimizer with in-memory storage for testing.
func NewInMemory() *TopologyOptimizer {
	return New(NewMemoryStore())
}

// RecordOutcome stores a build outcome for future analysis.
func (t *TopologyOptimizer) RecordOutcome(outcome types.BuildOutcome) error {
	if outcome.Timestamp.IsZero() {
		outcome.Timestamp = time.Now()
	}
	t.mu.Lock()
	t.outcomes = append(t.outcomes, outcome)
	t.mu.Unlock()
	return t.store.SaveOutcome(outcome)
}

// Recommend generates a model routing recommendation for a given pipeline phase
// based on contrastive analysis of historical outcomes.
func (t *TopologyOptimizer) Recommend(phase string) (types.TopologyRecommendation, error) {
	outcomes, err := t.store.LoadOutcomes(phase, 100)
	if err != nil {
		return types.TopologyRecommendation{}, fmt.Errorf("loading outcomes: %w", err)
	}

	if len(outcomes) == 0 {
		// No history: return default recommendation
		return t.defaultRecommendation(phase), nil
	}

	// Contrastive analysis: compare success vs failure rates per model
	type modelStats struct {
		totalRuns   int
		successes   int
		totalTurns  int
		totalScore  float64
		totalTimeMs int64
	}
	stats := make(map[string]*modelStats)

	for _, o := range outcomes {
		s, ok := stats[o.Model]
		if !ok {
			s = &modelStats{}
			stats[o.Model] = s
		}
		s.totalRuns++
		if o.Success {
			s.successes++
		}
		s.totalTurns += o.TurnsUsed
		s.totalScore += o.AuditScore
		s.totalTimeMs += o.Duration.Milliseconds()
	}

	// Score each model: success_rate * 0.5 + normalized_audit_score * 0.3 + efficiency * 0.2
	type modelScore struct {
		model      string
		score      float64
		successRate float64
		avgTurns   float64
		avgAudit   float64
	}
	var scores []modelScore
	for model, s := range stats {
		successRate := float64(s.successes) / float64(s.totalRuns)
		avgTurns := float64(s.totalTurns) / float64(s.totalRuns)
		avgAudit := s.totalScore / float64(s.totalRuns)
		// Efficiency: fewer turns is better (inverse, normalized)
		efficiency := 1.0 / math.Max(1.0, avgTurns/10.0)

		combined := successRate*0.5 + (avgAudit/100.0)*0.3 + efficiency*0.2
		scores = append(scores, modelScore{
			model:       model,
			score:       combined,
			successRate: successRate,
			avgTurns:    avgTurns,
			avgAudit:    avgAudit,
		})
	}

	sort.Slice(scores, func(i, j int) bool {
		return scores[i].score > scores[j].score
	})

	best := scores[0]
	return types.TopologyRecommendation{
		Phase:          phase,
		RecommendModel: best.model,
		TurnBudget:     int(math.Ceil(best.avgTurns * 1.2)), // 20% buffer
		Confidence:     math.Min(1.0, best.score),
		Reasoning: fmt.Sprintf("Based on %d runs: %s has %.0f%% success rate, avg audit %.1f, avg %.1f turns",
			stats[best.model].totalRuns, best.model, best.successRate*100, best.avgAudit, best.avgTurns),
	}, nil
}

// RecommendAll generates recommendations for all standard pipeline phases.
func (t *TopologyOptimizer) RecommendAll() ([]types.TopologyRecommendation, error) {
	phases := []string{"discover", "research", "synthesize", "build", "validate", "audit"}
	var recs []types.TopologyRecommendation

	for _, phase := range phases {
		rec, err := t.Recommend(phase)
		if err != nil {
			return nil, fmt.Errorf("recommending for %s: %w", phase, err)
		}
		recs = append(recs, rec)
	}
	return recs, nil
}

// TopologyDrift compares the current recommendation against a baseline and
// returns the drift magnitude (0 = stable, 1 = completely changed).
func (t *TopologyOptimizer) TopologyDrift(phase string, baselineModel string) (float64, error) {
	rec, err := t.Recommend(phase)
	if err != nil {
		return 0, err
	}

	if rec.RecommendModel == baselineModel {
		return 0, nil
	}
	// Different model recommended: drift proportional to confidence
	return rec.Confidence, nil
}

// Stats returns aggregate statistics about tracked outcomes.
func (t *TopologyOptimizer) Stats() (map[string]map[string]int, error) {
	outcomes, err := t.store.LoadAllOutcomes(0)
	if err != nil {
		return nil, err
	}

	// phase -> model -> count
	result := make(map[string]map[string]int)
	for _, o := range outcomes {
		if result[o.Phase] == nil {
			result[o.Phase] = make(map[string]int)
		}
		result[o.Phase][o.Model]++
	}
	return result, nil
}

// defaultRecommendation returns the default model for a phase when no history exists.
func (t *TopologyOptimizer) defaultRecommendation(phase string) types.TopologyRecommendation {
	switch phase {
	case "discover", "research":
		return types.TopologyRecommendation{
			Phase:          phase,
			RecommendModel: "haiku",
			TurnBudget:     10,
			Confidence:     0.5,
			Reasoning:      "default: haiku for discovery/research (fast, cost-effective)",
		}
	case "synthesize":
		return types.TopologyRecommendation{
			Phase:          phase,
			RecommendModel: "opus",
			TurnBudget:     15,
			Confidence:     0.5,
			Reasoning:      "default: opus for synthesis (highest quality reasoning)",
		}
	case "build":
		return types.TopologyRecommendation{
			Phase:          phase,
			RecommendModel: "sonnet",
			TurnBudget:     30,
			Confidence:     0.5,
			Reasoning:      "default: sonnet for building (good balance of speed and quality)",
		}
	case "validate", "audit":
		return types.TopologyRecommendation{
			Phase:          phase,
			RecommendModel: "sonnet",
			TurnBudget:     10,
			Confidence:     0.5,
			Reasoning:      "default: sonnet for validation/audit (reliable, efficient)",
		}
	default:
		return types.TopologyRecommendation{
			Phase:          phase,
			RecommendModel: "sonnet",
			TurnBudget:     15,
			Confidence:     0.3,
			Reasoning:      "default: sonnet (no phase-specific history)",
		}
	}
}

// Phases returns the standard pipeline phases.
func Phases() []string {
	return []string{"discover", "research", "synthesize", "build", "validate", "audit"}
}

// Models returns the available models.
func Models() []string {
	return append([]string{}, defaultModels...)
}
