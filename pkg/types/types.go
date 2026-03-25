// Package types defines shared data structures used across forge-oracle components.
package types

import "time"

// ProvenanceTier indicates the trustworthiness of a retrieved source.
type ProvenanceTier int

const (
	// TierPeerReviewed is the highest trust: peer-reviewed paper content.
	TierPeerReviewed ProvenanceTier = 1
	// TierHighStarRepo is moderate trust: high-star GitHub repos.
	TierHighStarRepo ProvenanceTier = 2
	// TierUnverified is lowest trust: unverified sources.
	TierUnverified ProvenanceTier = 3
)

// String returns a human-readable label for the provenance tier.
func (t ProvenanceTier) String() string {
	switch t {
	case TierPeerReviewed:
		return "peer-reviewed"
	case TierHighStarRepo:
		return "high-star-repo"
	case TierUnverified:
		return "unverified"
	default:
		return "unknown"
	}
}

// RetrievalCandidate represents a paper or repo returned by the discovery phase.
type RetrievalCandidate struct {
	// ID is a unique identifier (arXiv ID or repo full name).
	ID string
	// Title of the paper or repo.
	Title string
	// Abstract or description text.
	Abstract string
	// Source indicates where this came from ("arxiv", "github", "other").
	Source string
	// Keywords extracted from the candidate.
	Keywords []string
	// Stars is the GitHub star count (0 for papers).
	Stars int
	// Provenance is the trust tier assigned by Chain-of-Authorization tagging.
	Provenance ProvenanceTier
	// RelevanceScore is the probe-gradient reranking score (0-1).
	RelevanceScore float64
}

// ProductSpec represents a factory product specification used for build simulation
// and complexity calibration.
type ProductSpec struct {
	Name         string   `json:"name"`
	Description  string   `json:"description"`
	Language     string   `json:"language"`
	Features     []string `json:"features"`
	Techniques   []string `json:"techniques"`
	Dependencies []string `json:"dependencies"`
	Category     string   `json:"category"` // "cli", "library", "service", "agent"
	TurnBudget   int      `json:"turn_budget"`
}

// SimulationResult contains the output of a pre-flight build simulation.
type SimulationResult struct {
	// Confidence is the estimated probability of build success (0-1).
	Confidence float64
	// Feasible indicates whether the build is likely to succeed within budget.
	Feasible bool
	// Risks lists identified risk factors.
	Risks []string
	// ScopeReductions suggests features to cut if confidence is low.
	ScopeReductions []string
	// EstimatedTurns is the predicted number of turns needed.
	EstimatedTurns int
}

// FaultCategory classifies a build/test failure.
type FaultCategory string

const (
	FaultMissingDep   FaultCategory = "missing_dependency"
	FaultCompilation  FaultCategory = "compilation_error"
	FaultTestAssert   FaultCategory = "test_assertion"
	FaultImportError  FaultCategory = "import_error"
	FaultTimeout      FaultCategory = "timeout"
	FaultVetError     FaultCategory = "vet_error"
	FaultUnknown      FaultCategory = "unknown"
)

// FaultNode represents a node in a fault tree.
type FaultNode struct {
	// Category of this fault.
	Category FaultCategory
	// Message is the raw error text.
	Message string
	// File is the source file where the error occurred (if known).
	File string
	// Line is the line number (if known).
	Line int
	// Children are sub-causes (for AND/OR gate decomposition).
	Children []*FaultNode
	// Gate is "AND" or "OR" for composite nodes, empty for leaf nodes.
	Gate string
}

// FaultTree is the root of a fault analysis.
type FaultTree struct {
	Root      *FaultNode
	Summary   string
	FixPrompt string
}

// BuildOutcome records the result of a single build attempt for topology optimization.
type BuildOutcome struct {
	// ProductName identifies the product built.
	ProductName string
	// Model used ("opus", "sonnet", "haiku").
	Model string
	// Phase of the pipeline ("discover", "research", "synthesize", "build", "validate", "audit").
	Phase string
	// TurnsUsed is how many turns the agent consumed.
	TurnsUsed int
	// Success indicates whether the build passed.
	Success bool
	// AuditScore is the final audit score (0-100).
	AuditScore float64
	// Timestamp of the build.
	Timestamp time.Time
	// Duration of the build phase.
	Duration time.Duration
}

// TopologyRecommendation suggests model routing for a given phase.
type TopologyRecommendation struct {
	Phase          string
	RecommendModel string
	TurnBudget     int
	Confidence     float64
	Reasoning      string
}

// CalibrationResult is the output of complexity analysis.
type CalibrationResult struct {
	// ComplexityScore from 0 (trivial) to 1 (extremely complex).
	ComplexityScore float64
	// Buildable indicates if the spec is buildable within the turn budget.
	Buildable bool
	// EstimatedTurns is the predicted turns needed.
	EstimatedTurns int
	// SuggestedCuts are features to remove if over budget.
	SuggestedCuts []string
	// CategoryMatch indicates how well the spec matches its declared category.
	CategoryMatch float64
	// Reasoning explains the calibration decision.
	Reasoning string
}
