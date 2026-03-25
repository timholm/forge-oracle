package main

import (
	"fmt"
	"os"

	"github.com/timholm/forge-oracle/internal/calibrate"
	"github.com/timholm/forge-oracle/internal/diagnose"
	"github.com/timholm/forge-oracle/internal/guard"
	"github.com/timholm/forge-oracle/internal/simulate"
	"github.com/timholm/forge-oracle/internal/types"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Println("forge-oracle — factory pipeline quality layer")
		fmt.Println()
		fmt.Println("Commands:")
		fmt.Println("  guard      Score and filter retrieved papers/repos")
		fmt.Println("  simulate   Predict build success probability")
		fmt.Println("  diagnose   Parse test failures into root causes")
		fmt.Println("  calibrate  Check if a spec is buildable in N turns")
		os.Exit(0)
	}

	switch os.Args[1] {
	case "guard":
		runGuard()
	case "simulate":
		runSimulate()
	case "diagnose":
		runDiagnose()
	case "calibrate":
		runCalibrate()
	default:
		fmt.Fprintf(os.Stderr, "Unknown command: %s\n", os.Args[1])
		os.Exit(1)
	}
}

func runGuard() {
	g := guard.NewDefault()
	candidates := []types.RetrievalCandidate{
		{ID: "2603.12345", Title: "Code Generation with LLMs", Abstract: "We propose a method for code generation using large language models", Keywords: []string{"code", "generation", "llm"}},
		{ID: "2603.67890", Title: "Image Segmentation Survey", Abstract: "A survey of image segmentation methods", Keywords: []string{"image", "segmentation"}},
	}
	ranked := g.Rerank(candidates, []string{"code", "generation", "testing"})
	fmt.Println("Ranked papers:")
	for _, c := range ranked {
		fmt.Printf("  [%.2f] %s: %s\n", c.RelevanceScore, c.ID, c.Title)
	}
}

func runSimulate() {
	sim := simulate.NewDefault()
	spec := types.ProductSpec{
		Name:         "test-tool",
		Language:     "go",
		Features:     []string{"CLI", "HTTP API", "SQLite storage"},
		Techniques:   []string{"retrieval", "transformer"},
		Dependencies: []string{"cobra", "yaml"},
	}
	result := sim.Simulate(spec)
	fmt.Printf("Success probability: %.0f%%\n", result.Confidence*100)
	fmt.Printf("Estimated turns: %d\n", result.EstimatedTurns)
	if len(result.Risks) > 0 {
		fmt.Printf("Risks: %v\n", result.Risks)
	}
}

func runDiagnose() {
	d := diagnose.New()
	errorLog := `go test ./...
# github.com/example/foo/internal/bar
internal/bar/bar.go:15:2: no required module provides package github.com/spf13/cobra
FAIL github.com/example/foo [build failed]`
	tree := d.Parse(errorLog)
	fmt.Printf("Category: %s\n", tree.Root.Category)
	fmt.Printf("Summary: %s\n", tree.Summary)
	fmt.Printf("Fix prompt: %s\n", tree.FixPrompt[:min(len(tree.FixPrompt), 200)])
}

func runCalibrate() {
	c := calibrate.NewDefault()
	spec := types.ProductSpec{
		Name:       "complex-tool",
		Language:   "go",
		Features:   make([]string, 20),
		Techniques: make([]string, 7),
	}
	result := c.Calibrate(spec)
	fmt.Printf("Buildable: %v\n", result.Buildable)
	fmt.Printf("Estimated turns: %d\n", result.EstimatedTurns)
	fmt.Printf("Complexity: %.1f\n", result.ComplexityScore)
	if len(result.SuggestedCuts) > 0 {
		fmt.Printf("Suggested cuts: %v\n", result.SuggestedCuts)
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
