// Package calibrate implements ComplexityCalibrator: analyzes a ProductSpec
// and estimates if it's buildable within N turns, suggesting feature cuts when needed.
//
// Technique applied:
//   - Minibal balanced calibration (arXiv:2603.23059): calibrate product complexity
//     to match the target audience and deployment context, avoiding over-engineering.
package calibrate

import (
	"fmt"
	"math"
	"strings"

	"github.com/timholm/forge-oracle/internal/types"
)

// categoryBudgets maps product categories to their complexity budgets.
// A CLI tool should be simpler than a distributed service.
var categoryBudgets = map[string]float64{
	"cli":     0.35,
	"library": 0.55,
	"service": 0.70,
	"agent":   0.80,
}

// Config controls calibrator behavior.
type Config struct {
	// TurnsPerComplexityPoint maps complexity score to required turns.
	TurnsPerComplexityPoint float64
	// BaseTurns is the minimum turns for any build.
	BaseTurns int
	// MaxFeatures is the maximum reasonable features for a single build.
	MaxFeatures int
	// MaxTechniques is the maximum reasonable techniques for a single build.
	MaxTechniques int
	// MaxDependencies is the maximum reasonable dependencies.
	MaxDependencies int
}

// DefaultConfig returns production defaults.
func DefaultConfig() Config {
	return Config{
		TurnsPerComplexityPoint: 40.0,
		BaseTurns:               3,
		MaxFeatures:             15,
		MaxTechniques:           7,
		MaxDependencies:         20,
	}
}

// ComplexityCalibrator analyzes ProductSpecs against complexity budgets derived
// from the product category and outputs scoping recommendations.
type ComplexityCalibrator struct {
	config Config
}

// New creates a ComplexityCalibrator with the given configuration.
func New(cfg Config) *ComplexityCalibrator {
	return &ComplexityCalibrator{config: cfg}
}

// NewDefault creates a ComplexityCalibrator with default configuration.
func NewDefault() *ComplexityCalibrator {
	return New(DefaultConfig())
}

// Calibrate analyzes a ProductSpec and returns a CalibrationResult indicating
// whether it's buildable within the turn budget, with suggestions if not.
func (c *ComplexityCalibrator) Calibrate(spec types.ProductSpec) types.CalibrationResult {
	result := types.CalibrationResult{}

	// Compute complexity score from multiple dimensions
	featureScore := c.featureComplexity(spec)
	techniqueScore := c.techniqueComplexity(spec)
	depScore := c.dependencyComplexity(spec)
	languageScore := c.languageComplexity(spec.Language)

	// Weighted combination
	result.ComplexityScore = featureScore*0.35 + techniqueScore*0.30 + depScore*0.15 + languageScore*0.20
	result.ComplexityScore = math.Min(1.0, math.Max(0.0, result.ComplexityScore))

	// Category match: how well does complexity match the expected budget?
	budget, ok := categoryBudgets[strings.ToLower(spec.Category)]
	if !ok {
		budget = 0.5
	}
	if result.ComplexityScore <= budget {
		result.CategoryMatch = 1.0 - (budget-result.ComplexityScore)/budget
	} else {
		result.CategoryMatch = math.Max(0, 1.0-(result.ComplexityScore-budget)/(1.0-budget))
	}

	// Estimate turns needed
	result.EstimatedTurns = c.config.BaseTurns + int(math.Ceil(result.ComplexityScore*c.config.TurnsPerComplexityPoint))

	// Check buildability
	turnBudget := spec.TurnBudget
	if turnBudget <= 0 {
		turnBudget = 30 // default
	}
	result.Buildable = result.EstimatedTurns <= turnBudget

	// Generate reasoning
	result.Reasoning = c.generateReasoning(spec, result, budget)

	// Suggest cuts if not buildable
	if !result.Buildable {
		result.SuggestedCuts = c.suggestCuts(spec, result.EstimatedTurns, turnBudget)
	}

	return result
}

// featureComplexity scores the feature set (0-1).
func (c *ComplexityCalibrator) featureComplexity(spec types.ProductSpec) float64 {
	n := len(spec.Features)
	if n == 0 {
		return 0
	}

	// Base score from count
	countScore := math.Min(1.0, float64(n)/float64(c.config.MaxFeatures))

	// Complexity indicators in feature descriptions
	complexWords := []string{
		"distributed", "concurrent", "real-time", "streaming", "encryption",
		"authentication", "machine learning", "neural", "gpu", "cluster",
		"consensus", "persistent", "replicated", "sharded",
	}
	complexCount := 0
	totalWords := 0
	for _, f := range spec.Features {
		words := strings.Fields(strings.ToLower(f))
		totalWords += len(words)
		for _, w := range words {
			for _, cw := range complexWords {
				if strings.Contains(w, cw) {
					complexCount++
					break
				}
			}
		}
	}

	wordRatio := 0.0
	if totalWords > 0 {
		wordRatio = math.Min(1.0, float64(complexCount)/float64(n))
	}

	return countScore*0.6 + wordRatio*0.4
}

// techniqueComplexity scores the technique set (0-1).
func (c *ComplexityCalibrator) techniqueComplexity(spec types.ProductSpec) float64 {
	n := len(spec.Techniques)
	if n == 0 {
		return 0
	}
	return math.Min(1.0, float64(n)/float64(c.config.MaxTechniques))
}

// dependencyComplexity scores the dependency set (0-1).
func (c *ComplexityCalibrator) dependencyComplexity(spec types.ProductSpec) float64 {
	n := len(spec.Dependencies)
	if n == 0 {
		return 0
	}
	return math.Min(1.0, float64(n)/float64(c.config.MaxDependencies))
}

// languageComplexity scores how hard a language is to generate code for (0-1).
func (c *ComplexityCalibrator) languageComplexity(lang string) float64 {
	// Languages harder for LLMs to generate get higher complexity scores
	hardness := map[string]float64{
		"go":         0.25,
		"python":     0.15,
		"typescript": 0.30,
		"javascript": 0.28,
		"rust":       0.60,
		"c":          0.65,
		"cpp":        0.70,
		"c++":        0.70,
		"java":       0.35,
		"haskell":    0.75,
		"zig":        0.80,
	}
	if h, ok := hardness[strings.ToLower(strings.TrimSpace(lang))]; ok {
		return h
	}
	return 0.50 // Unknown language gets moderate complexity
}

// suggestCuts recommends features to remove to fit the turn budget.
func (c *ComplexityCalibrator) suggestCuts(spec types.ProductSpec, estimated, budget int) []string {
	var cuts []string
	turnsToSave := estimated - budget

	turnsPerFeature := 0.0
	if len(spec.Features) > 0 {
		turnsPerFeature = float64(estimated-c.config.BaseTurns) / float64(len(spec.Features)+len(spec.Techniques))
	}
	if turnsPerFeature <= 0 {
		turnsPerFeature = 2.0
	}

	// Rank features by word count (proxy for complexity) — longer = more complex = cut first
	type rankedFeature struct {
		index     int
		feature   string
		wordCount int
	}
	var ranked []rankedFeature
	for i, f := range spec.Features {
		ranked = append(ranked, rankedFeature{i, f, len(strings.Fields(f))})
	}
	// Sort by word count descending (most complex first)
	for i := 0; i < len(ranked); i++ {
		for j := i + 1; j < len(ranked); j++ {
			if ranked[j].wordCount > ranked[i].wordCount {
				ranked[i], ranked[j] = ranked[j], ranked[i]
			}
		}
	}

	saved := 0.0
	for _, rf := range ranked {
		if saved >= float64(turnsToSave) {
			break
		}
		if len(spec.Features)-len(cuts) <= 1 {
			break // Keep at least one feature
		}
		cuts = append(cuts, fmt.Sprintf("remove: %s (saves ~%.0f turns)", rf.feature, turnsPerFeature))
		saved += turnsPerFeature
	}

	if saved < float64(turnsToSave) && len(spec.Techniques) > 3 {
		turnsSaved := float64(len(spec.Techniques)-3) * turnsPerFeature
		cuts = append(cuts, fmt.Sprintf("reduce techniques from %d to 3 (saves ~%.0f turns)", len(spec.Techniques), turnsSaved))
	}

	return cuts
}

// generateReasoning produces a human-readable explanation of the calibration.
func (c *ComplexityCalibrator) generateReasoning(spec types.ProductSpec, result types.CalibrationResult, budget float64) string {
	var parts []string

	parts = append(parts, fmt.Sprintf("Complexity: %.0f%% (budget for %q: %.0f%%)",
		result.ComplexityScore*100, spec.Category, budget*100))

	if result.ComplexityScore > budget {
		parts = append(parts, fmt.Sprintf("OVER BUDGET by %.0f%% — spec is too complex for category %q",
			(result.ComplexityScore-budget)*100, spec.Category))
	}

	parts = append(parts, fmt.Sprintf("Estimated turns: %d (budget: %d)",
		result.EstimatedTurns, spec.TurnBudget))

	if len(spec.Features) > c.config.MaxFeatures/2 {
		parts = append(parts, fmt.Sprintf("High feature count (%d) contributing to complexity", len(spec.Features)))
	}

	if len(spec.Techniques) > c.config.MaxTechniques/2 {
		parts = append(parts, fmt.Sprintf("High technique count (%d) contributing to complexity", len(spec.Techniques)))
	}

	if result.Buildable {
		parts = append(parts, "VERDICT: buildable within turn budget")
	} else {
		parts = append(parts, "VERDICT: NOT buildable — scope reduction required")
	}

	return strings.Join(parts, ". ")
}
