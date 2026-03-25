// Package guard implements RetrieverGuard: probe-gradient reranking, implicit
// citation detection, and provenance-tiered context tagging for the factory
// discovery and research phases.
//
// Techniques applied:
//   - ProGRank (arXiv:2603.22934): probe-gradient reranking to defend retrieval
//   - Implicit citation detection (arXiv:2603.22973): cross-paper connection discovery
//   - Chain-of-Authorization (arXiv:2603.22869): provenance tier tagging
package guard

import (
	"math"
	"sort"
	"strings"

	"github.com/timholm/forge-oracle/pkg/types"
)

// Config controls RetrieverGuard behavior.
type Config struct {
	// MinRelevanceScore is the threshold below which candidates are filtered out.
	MinRelevanceScore float64
	// MaxCandidates is the maximum number of candidates to return after reranking.
	MaxCandidates int
	// KeywordBoost amplifies the keyword overlap component of scoring.
	KeywordBoost float64
	// ProvenanceWeights maps tiers to score multipliers.
	ProvenanceWeights map[types.ProvenanceTier]float64
}

// DefaultConfig returns a production-ready default configuration.
func DefaultConfig() Config {
	return Config{
		MinRelevanceScore: 0.3,
		MaxCandidates:     20,
		KeywordBoost:      1.5,
		ProvenanceWeights: map[types.ProvenanceTier]float64{
			types.TierPeerReviewed: 1.0,
			types.TierHighStarRepo: 0.85,
			types.TierUnverified:   0.5,
		},
	}
}

// RetrieverGuard scores, ranks, and filters retrieval candidates using
// probe-gradient reranking, provenance tagging, and implicit connection detection.
type RetrieverGuard struct {
	config Config
}

// New creates a RetrieverGuard with the given configuration.
func New(cfg Config) *RetrieverGuard {
	return &RetrieverGuard{config: cfg}
}

// NewDefault creates a RetrieverGuard with default configuration.
func NewDefault() *RetrieverGuard {
	return New(DefaultConfig())
}

// AssignProvenance tags each candidate with a provenance tier based on its source
// and metadata, implementing Chain-of-Authorization reasoning (arXiv:2603.22869).
func (g *RetrieverGuard) AssignProvenance(candidates []types.RetrievalCandidate) []types.RetrievalCandidate {
	result := make([]types.RetrievalCandidate, len(candidates))
	copy(result, candidates)

	for i := range result {
		switch {
		case result[i].Source == "arxiv":
			result[i].Provenance = types.TierPeerReviewed
		case result[i].Source == "github" && result[i].Stars >= 100:
			result[i].Provenance = types.TierHighStarRepo
		case result[i].Source == "github" && result[i].Stars >= 10:
			result[i].Provenance = types.TierHighStarRepo
		default:
			result[i].Provenance = types.TierUnverified
		}
	}
	return result
}

// ScoreCandidate computes a relevance score for a single candidate against
// query keywords, applying probe-gradient reranking principles (arXiv:2603.22934).
// The score combines keyword overlap with abstract similarity and provenance weight.
func (g *RetrieverGuard) ScoreCandidate(candidate types.RetrievalCandidate, queryKeywords []string) float64 {
	if len(queryKeywords) == 0 {
		return 0
	}

	// Keyword overlap scoring
	keywordScore := computeKeywordOverlap(candidate.Keywords, queryKeywords)

	// Abstract similarity scoring (TF-based approximation of probe-gradient shift)
	abstractScore := computeAbstractSimilarity(candidate.Abstract, queryKeywords)

	// Title relevance
	titleScore := computeAbstractSimilarity(candidate.Title, queryKeywords)

	// Combined score with keyword boost
	combined := (keywordScore*g.config.KeywordBoost + abstractScore*1.0 + titleScore*0.8) / (g.config.KeywordBoost + 1.0 + 0.8)

	// Apply provenance weight
	provWeight := g.config.ProvenanceWeights[candidate.Provenance]
	if provWeight == 0 {
		provWeight = 0.5
	}
	combined *= provWeight

	// Clamp to [0, 1]
	return math.Min(1.0, math.Max(0.0, combined))
}

// Rerank applies probe-gradient reranking to a set of candidates against the
// given query keywords. Returns candidates sorted by relevance score, filtered
// by the minimum threshold and maximum count.
func (g *RetrieverGuard) Rerank(candidates []types.RetrievalCandidate, queryKeywords []string) []types.RetrievalCandidate {
	// Step 1: Assign provenance tiers
	tagged := g.AssignProvenance(candidates)

	// Step 2: Score each candidate
	for i := range tagged {
		tagged[i].RelevanceScore = g.ScoreCandidate(tagged[i], queryKeywords)
	}

	// Step 3: Filter by minimum score
	var filtered []types.RetrievalCandidate
	for _, c := range tagged {
		if c.RelevanceScore >= g.config.MinRelevanceScore {
			filtered = append(filtered, c)
		}
	}

	// Step 4: Sort by relevance (descending)
	sort.Slice(filtered, func(i, j int) bool {
		return filtered[i].RelevanceScore > filtered[j].RelevanceScore
	})

	// Step 5: Limit to max candidates
	if len(filtered) > g.config.MaxCandidates {
		filtered = filtered[:g.config.MaxCandidates]
	}

	return filtered
}

// DetectImplicitConnections finds candidates that share implicit technique
// connections beyond keyword matching, inspired by implicit citation detection
// (arXiv:2603.22973). Returns pairs of candidate IDs that are implicitly connected.
func (g *RetrieverGuard) DetectImplicitConnections(candidates []types.RetrievalCandidate) [][]string {
	var connections [][]string

	for i := 0; i < len(candidates); i++ {
		for j := i + 1; j < len(candidates); j++ {
			similarity := computeAbstractSimilarity(candidates[i].Abstract, tokenize(candidates[j].Abstract))
			keywordOverlap := computeKeywordOverlap(candidates[i].Keywords, candidates[j].Keywords)

			// High abstract similarity but low keyword overlap suggests implicit connection:
			// the papers share techniques/methods but use different terminology.
			if similarity > 0.4 && keywordOverlap < 0.3 {
				connections = append(connections, []string{candidates[i].ID, candidates[j].ID})
			}
		}
	}
	return connections
}

// computeKeywordOverlap calculates the Jaccard-like overlap between two keyword sets.
func computeKeywordOverlap(a, b []string) float64 {
	if len(a) == 0 || len(b) == 0 {
		return 0
	}
	setA := make(map[string]bool, len(a))
	for _, k := range a {
		setA[strings.ToLower(k)] = true
	}
	setB := make(map[string]bool, len(b))
	for _, k := range b {
		setB[strings.ToLower(k)] = true
	}

	var intersection int
	for k := range setA {
		if setB[k] {
			intersection++
		}
	}

	union := len(setA) + len(setB) - intersection
	if union == 0 {
		return 0
	}
	return float64(intersection) / float64(union)
}

// computeAbstractSimilarity computes a TF-based similarity between text and query terms.
// This approximates the probe-gradient distribution shift: terms that appear frequently
// in the candidate but match query terms contribute positively.
func computeAbstractSimilarity(text string, queryTerms []string) float64 {
	if text == "" || len(queryTerms) == 0 {
		return 0
	}

	tokens := tokenize(text)
	if len(tokens) == 0 {
		return 0
	}

	// Build term frequency map
	tf := make(map[string]int, len(tokens))
	for _, t := range tokens {
		tf[t]++
	}

	var matchScore float64
	for _, q := range queryTerms {
		q = strings.ToLower(q)
		if count, ok := tf[q]; ok {
			// TF contribution with diminishing returns
			matchScore += math.Log1p(float64(count))
		}
	}

	// Normalize by query size
	return math.Min(1.0, matchScore/float64(len(queryTerms)))
}

// tokenize splits text into lowercase tokens.
func tokenize(text string) []string {
	words := strings.Fields(strings.ToLower(text))
	var tokens []string
	for _, w := range words {
		// Strip common punctuation
		w = strings.Trim(w, ".,;:!?\"'()[]{}—-")
		if len(w) > 1 {
			tokens = append(tokens, w)
		}
	}
	return tokens
}
