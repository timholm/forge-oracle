// Package simulate implements WorldModelSimulator: pre-flight build simulation
// that estimates success probability before spending agent turns.
//
// Technique applied:
//   - Describe-Then-Act (arXiv:2603.23149): distilled language-action world models
//     predict build outcomes before execution, enabling proactive steering.
package simulate

import (
	"fmt"
	"math"
	"strings"

	"github.com/timholm/forge-oracle/pkg/types"
)

// languageSupport maps languages to a support quality score (how well LLMs handle them).
var languageSupport = map[string]float64{
	"go":         0.95,
	"python":     0.98,
	"typescript": 0.92,
	"javascript": 0.92,
	"rust":       0.80,
	"java":       0.88,
	"c":          0.75,
	"cpp":        0.75,
	"c++":        0.75,
	"ruby":       0.85,
	"swift":      0.78,
	"kotlin":     0.82,
	"zig":        0.60,
	"haskell":    0.65,
}

// Config controls simulator behavior.
type Config struct {
	// DefaultTurnBudget is the assumed turn budget when spec doesn't specify one.
	DefaultTurnBudget int
	// TurnsPerFeature is the estimated turns needed per feature.
	TurnsPerFeature float64
	// TurnsPerDependency is the estimated turns added per external dependency.
	TurnsPerDependency float64
	// TurnsPerTechnique is the estimated turns added per research technique.
	TurnsPerTechnique float64
	// ConfidenceThreshold below which the build is considered not feasible.
	ConfidenceThreshold float64
}

// DefaultConfig returns production defaults for the simulator.
func DefaultConfig() Config {
	return Config{
		DefaultTurnBudget:   30,
		TurnsPerFeature:     2.5,
		TurnsPerDependency:  0.5,
		TurnsPerTechnique:   3.0,
		ConfidenceThreshold: 0.5,
	}
}

// WorldModelSimulator estimates build success probability for a ProductSpec
// by analyzing spec complexity against the turn budget, language support,
// dependency count, and technique feasibility.
type WorldModelSimulator struct {
	config Config
}

// New creates a WorldModelSimulator with the given configuration.
func New(cfg Config) *WorldModelSimulator {
	return &WorldModelSimulator{config: cfg}
}

// NewDefault creates a WorldModelSimulator with default configuration.
func NewDefault() *WorldModelSimulator {
	return New(DefaultConfig())
}

// Simulate runs pre-flight analysis on a ProductSpec and returns a SimulationResult
// with confidence score, feasibility assessment, risks, and scope reduction suggestions.
func (s *WorldModelSimulator) Simulate(spec types.ProductSpec) types.SimulationResult {
	result := types.SimulationResult{}

	turnBudget := spec.TurnBudget
	if turnBudget <= 0 {
		turnBudget = s.config.DefaultTurnBudget
	}

	// Estimate required turns
	estimatedTurns := s.estimateTurns(spec)
	result.EstimatedTurns = estimatedTurns

	// Language support factor
	langFactor := s.languageFactor(spec.Language)

	// Complexity factors
	featureComplexity := s.featureComplexity(spec)
	depRisk := s.dependencyRisk(spec)
	techniqueRisk := s.techniqueRisk(spec)

	// Compute turn ratio: budget / estimated (capped at 1.0 means we have enough)
	turnRatio := float64(turnBudget) / math.Max(1, float64(estimatedTurns))
	turnConfidence := math.Min(1.0, turnRatio)

	// Overall confidence: weighted combination
	confidence := turnConfidence*0.35 +
		langFactor*0.15 +
		(1.0-featureComplexity)*0.20 +
		(1.0-depRisk)*0.15 +
		(1.0-techniqueRisk)*0.15

	result.Confidence = math.Min(1.0, math.Max(0.0, confidence))
	result.Feasible = result.Confidence >= s.config.ConfidenceThreshold

	// Identify risks
	if turnConfidence < 0.7 {
		result.Risks = append(result.Risks, fmt.Sprintf("turn budget (%d) may be insufficient (estimated %d turns needed)", turnBudget, estimatedTurns))
	}
	if langFactor < 0.75 {
		result.Risks = append(result.Risks, fmt.Sprintf("language %q has limited LLM support (%.0f%%)", spec.Language, langFactor*100))
	}
	if depRisk > 0.5 {
		result.Risks = append(result.Risks, fmt.Sprintf("high dependency count (%d) increases failure risk", len(spec.Dependencies)))
	}
	if techniqueRisk > 0.5 {
		result.Risks = append(result.Risks, fmt.Sprintf("complex technique set (%d techniques) may exceed scope", len(spec.Techniques)))
	}
	if featureComplexity > 0.7 {
		result.Risks = append(result.Risks, fmt.Sprintf("high feature complexity (%.0f%%) — consider reducing scope", featureComplexity*100))
	}

	// Generate scope reductions if not feasible
	if !result.Feasible {
		result.ScopeReductions = s.suggestScopeReductions(spec, turnBudget, estimatedTurns)
	}

	return result
}

// estimateTurns predicts how many turns a spec will require.
func (s *WorldModelSimulator) estimateTurns(spec types.ProductSpec) int {
	turns := 2.0 // Base: project setup
	turns += float64(len(spec.Features)) * s.config.TurnsPerFeature
	turns += float64(len(spec.Dependencies)) * s.config.TurnsPerDependency
	turns += float64(len(spec.Techniques)) * s.config.TurnsPerTechnique
	return int(math.Ceil(turns))
}

// languageFactor returns the LLM support quality for a language.
func (s *WorldModelSimulator) languageFactor(lang string) float64 {
	lang = strings.ToLower(strings.TrimSpace(lang))
	if score, ok := languageSupport[lang]; ok {
		return score
	}
	return 0.6 // Unknown language gets conservative score
}

// featureComplexity estimates the complexity contribution of features (0-1).
func (s *WorldModelSimulator) featureComplexity(spec types.ProductSpec) float64 {
	if len(spec.Features) == 0 {
		return 0
	}

	// Count features with complexity indicators
	complexIndicators := []string{"distributed", "concurrent", "real-time", "streaming",
		"encryption", "authentication", "machine learning", "neural", "gpu",
		"cluster", "consensus", "blockchain"}

	complexCount := 0
	for _, f := range spec.Features {
		fLower := strings.ToLower(f)
		for _, indicator := range complexIndicators {
			if strings.Contains(fLower, indicator) {
				complexCount++
				break
			}
		}
	}

	// Base complexity from feature count + complex feature ratio
	countFactor := math.Min(1.0, float64(len(spec.Features))/15.0)
	complexFactor := float64(complexCount) / float64(len(spec.Features))
	return countFactor*0.6 + complexFactor*0.4
}

// dependencyRisk estimates risk from external dependencies (0-1).
func (s *WorldModelSimulator) dependencyRisk(spec types.ProductSpec) float64 {
	n := len(spec.Dependencies)
	if n == 0 {
		return 0
	}
	// Risk grows logarithmically: many deps = higher chance of version conflicts
	return math.Min(1.0, math.Log1p(float64(n))/math.Log(20))
}

// techniqueRisk estimates risk from research technique complexity (0-1).
func (s *WorldModelSimulator) techniqueRisk(spec types.ProductSpec) float64 {
	n := len(spec.Techniques)
	if n == 0 {
		return 0
	}
	return math.Min(1.0, float64(n)/10.0)
}

// suggestScopeReductions identifies features to cut to fit within the turn budget.
func (s *WorldModelSimulator) suggestScopeReductions(spec types.ProductSpec, budget, estimated int) []string {
	if len(spec.Features) <= 1 {
		return []string{"increase turn budget or simplify the single feature"}
	}

	turnsToSave := estimated - budget
	var suggestions []string

	// Suggest cutting features starting from the end (assumed least critical)
	saved := 0.0
	for i := len(spec.Features) - 1; i >= 1 && saved < float64(turnsToSave); i-- {
		suggestions = append(suggestions, fmt.Sprintf("remove feature: %s (saves ~%.0f turns)", spec.Features[i], s.config.TurnsPerFeature))
		saved += s.config.TurnsPerFeature
	}

	if len(spec.Techniques) > 3 {
		suggestions = append(suggestions, fmt.Sprintf("reduce techniques from %d to 3 (saves ~%.0f turns)",
			len(spec.Techniques), float64(len(spec.Techniques)-3)*s.config.TurnsPerTechnique))
	}

	return suggestions
}
