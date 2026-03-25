// Package diagnose implements FaultTreeBuilder: structured fault tree construction
// from build/test failure output with targeted retry prompt generation.
//
// Technique applied:
//   - JFTA-Bench fault tree analysis (arXiv:2603.22978): decompose failures into
//     AND/OR fault trees for root cause diagnosis and targeted fix prompts.
package diagnose

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/timholm/forge-oracle/internal/types"
)

// pattern matchers for common Go build/test errors.
var (
	reUndeclared   = regexp.MustCompile(`(?m)^(.+?):(\d+):\d+: undefined: (.+)$`)
	reImport       = regexp.MustCompile(`(?m)^(.+?):(\d+):\d+: could not import (.+)$`)
	reNoPackage    = regexp.MustCompile(`(?m)cannot find package "(.+?)"`)
	reNoModule     = regexp.MustCompile(`(?m)no required module provides package (.+?);`)
	reCompile      = regexp.MustCompile(`(?m)^(.+?):(\d+):\d+: (.+)$`)
	reTestFail     = regexp.MustCompile(`(?m)^--- FAIL: (.+?) \((.+?)\)$`)
	reTestPanic    = regexp.MustCompile(`(?m)^panic: (.+)$`)
	reVetError     = regexp.MustCompile(`(?m)^#.+?\n(.+?):(\d+):\d+: (.+)$`)
	reTimeout      = regexp.MustCompile(`(?mi)(timed? ?out|deadline exceeded|context deadline|timeout)`)
	reBuildFailed  = regexp.MustCompile(`(?m)^FAIL\s+(.+?)\s+`)
	reAssertFail   = regexp.MustCompile(`(?m)^\s+(.+?):(\d+): (.+)$`)
	reGoModMissing = regexp.MustCompile(`(?m)missing go.sum entry for module providing package (.+)`)
)

// FaultTreeBuilder parses build/test error output and constructs a structured
// fault tree that decomposes failures into categorized root causes.
type FaultTreeBuilder struct{}

// New creates a new FaultTreeBuilder.
func New() *FaultTreeBuilder {
	return &FaultTreeBuilder{}
}

// Parse takes raw error output from `make test` or `go build` and constructs
// a FaultTree with categorized failure nodes and a targeted fix prompt.
func (b *FaultTreeBuilder) Parse(errorOutput string) types.FaultTree {
	tree := types.FaultTree{
		Root: &types.FaultNode{
			Category: types.FaultUnknown,
			Message:  "Build/test failure",
			Gate:     "OR",
		},
	}

	// Parse different fault categories
	missingDeps := b.parseMissingDeps(errorOutput)
	importErrors := b.parseImportErrors(errorOutput)
	compilationErrors := b.parseCompilationErrors(errorOutput)
	testFailures := b.parseTestFailures(errorOutput)
	timeouts := b.parseTimeouts(errorOutput)
	vetErrors := b.parseVetErrors(errorOutput)

	// Attach to tree
	if len(missingDeps) > 0 {
		depNode := &types.FaultNode{
			Category: types.FaultMissingDep,
			Message:  fmt.Sprintf("%d missing dependencies", len(missingDeps)),
			Gate:     "AND",
			Children: missingDeps,
		}
		tree.Root.Children = append(tree.Root.Children, depNode)
	}
	if len(importErrors) > 0 {
		importNode := &types.FaultNode{
			Category: types.FaultImportError,
			Message:  fmt.Sprintf("%d import errors", len(importErrors)),
			Gate:     "AND",
			Children: importErrors,
		}
		tree.Root.Children = append(tree.Root.Children, importNode)
	}
	if len(compilationErrors) > 0 {
		compNode := &types.FaultNode{
			Category: types.FaultCompilation,
			Message:  fmt.Sprintf("%d compilation errors", len(compilationErrors)),
			Gate:     "AND",
			Children: compilationErrors,
		}
		tree.Root.Children = append(tree.Root.Children, compNode)
	}
	if len(testFailures) > 0 {
		testNode := &types.FaultNode{
			Category: types.FaultTestAssert,
			Message:  fmt.Sprintf("%d test failures", len(testFailures)),
			Gate:     "OR",
			Children: testFailures,
		}
		tree.Root.Children = append(tree.Root.Children, testNode)
	}
	if len(timeouts) > 0 {
		timeoutNode := &types.FaultNode{
			Category: types.FaultTimeout,
			Message:  fmt.Sprintf("%d timeout errors", len(timeouts)),
			Gate:     "OR",
			Children: timeouts,
		}
		tree.Root.Children = append(tree.Root.Children, timeoutNode)
	}
	if len(vetErrors) > 0 {
		vetNode := &types.FaultNode{
			Category: types.FaultVetError,
			Message:  fmt.Sprintf("%d vet errors", len(vetErrors)),
			Gate:     "AND",
			Children: vetErrors,
		}
		tree.Root.Children = append(tree.Root.Children, vetNode)
	}

	tree.Summary = b.generateSummary(tree.Root)
	tree.FixPrompt = b.generateFixPrompt(tree.Root)

	return tree
}

// Categorize determines the primary fault category from error output.
func (b *FaultTreeBuilder) Categorize(errorOutput string) types.FaultCategory {
	tree := b.Parse(errorOutput)
	if len(tree.Root.Children) == 0 {
		return types.FaultUnknown
	}
	// Return the category of the first (highest priority) child
	return tree.Root.Children[0].Category
}

func (b *FaultTreeBuilder) parseMissingDeps(output string) []*types.FaultNode {
	var nodes []*types.FaultNode
	seen := make(map[string]bool)

	for _, matches := range reNoModule.FindAllStringSubmatch(output, -1) {
		pkg := matches[1]
		if !seen[pkg] {
			seen[pkg] = true
			nodes = append(nodes, &types.FaultNode{
				Category: types.FaultMissingDep,
				Message:  fmt.Sprintf("missing module for package: %s", pkg),
			})
		}
	}
	for _, matches := range reNoPackage.FindAllStringSubmatch(output, -1) {
		pkg := matches[1]
		if !seen[pkg] {
			seen[pkg] = true
			nodes = append(nodes, &types.FaultNode{
				Category: types.FaultMissingDep,
				Message:  fmt.Sprintf("cannot find package: %s", pkg),
			})
		}
	}
	for _, matches := range reGoModMissing.FindAllStringSubmatch(output, -1) {
		pkg := matches[1]
		if !seen[pkg] {
			seen[pkg] = true
			nodes = append(nodes, &types.FaultNode{
				Category: types.FaultMissingDep,
				Message:  fmt.Sprintf("missing go.sum entry: %s", pkg),
			})
		}
	}
	return nodes
}

func (b *FaultTreeBuilder) parseImportErrors(output string) []*types.FaultNode {
	var nodes []*types.FaultNode
	seen := make(map[string]bool)

	for _, matches := range reImport.FindAllStringSubmatch(output, -1) {
		key := matches[1] + ":" + matches[3]
		if !seen[key] {
			seen[key] = true
			nodes = append(nodes, &types.FaultNode{
				Category: types.FaultImportError,
				Message:  fmt.Sprintf("could not import %s", matches[3]),
				File:     matches[1],
			})
		}
	}
	return nodes
}

func (b *FaultTreeBuilder) parseCompilationErrors(output string) []*types.FaultNode {
	var nodes []*types.FaultNode
	seen := make(map[string]bool)

	for _, matches := range reUndeclared.FindAllStringSubmatch(output, -1) {
		key := matches[1] + ":" + matches[2] + ":" + matches[3]
		if !seen[key] {
			seen[key] = true
			lineNum := 0
			fmt.Sscanf(matches[2], "%d", &lineNum)
			nodes = append(nodes, &types.FaultNode{
				Category: types.FaultCompilation,
				Message:  fmt.Sprintf("undefined: %s", matches[3]),
				File:     matches[1],
				Line:     lineNum,
			})
		}
	}
	return nodes
}

func (b *FaultTreeBuilder) parseTestFailures(output string) []*types.FaultNode {
	var nodes []*types.FaultNode

	for _, matches := range reTestFail.FindAllStringSubmatch(output, -1) {
		nodes = append(nodes, &types.FaultNode{
			Category: types.FaultTestAssert,
			Message:  fmt.Sprintf("FAIL: %s (%s)", matches[1], matches[2]),
		})
	}

	if reTestPanic.MatchString(output) {
		panicMatches := reTestPanic.FindStringSubmatch(output)
		nodes = append(nodes, &types.FaultNode{
			Category: types.FaultTestAssert,
			Message:  fmt.Sprintf("panic: %s", panicMatches[1]),
		})
	}

	return nodes
}

func (b *FaultTreeBuilder) parseTimeouts(output string) []*types.FaultNode {
	var nodes []*types.FaultNode
	if reTimeout.MatchString(output) {
		nodes = append(nodes, &types.FaultNode{
			Category: types.FaultTimeout,
			Message:  "build or test timed out",
		})
	}
	return nodes
}

func (b *FaultTreeBuilder) parseVetErrors(output string) []*types.FaultNode {
	var nodes []*types.FaultNode
	for _, matches := range reVetError.FindAllStringSubmatch(output, -1) {
		lineNum := 0
		fmt.Sscanf(matches[2], "%d", &lineNum)
		nodes = append(nodes, &types.FaultNode{
			Category: types.FaultVetError,
			Message:  matches[3],
			File:     matches[1],
			Line:     lineNum,
		})
	}
	return nodes
}

// generateSummary creates a human-readable summary of the fault tree.
func (b *FaultTreeBuilder) generateSummary(root *types.FaultNode) string {
	if len(root.Children) == 0 {
		return "No specific faults detected. Check error output manually."
	}

	var parts []string
	for _, child := range root.Children {
		parts = append(parts, child.Message)
	}
	return fmt.Sprintf("Root cause analysis: %s", strings.Join(parts, "; "))
}

// generateFixPrompt creates a targeted Claude prompt to fix the diagnosed faults.
func (b *FaultTreeBuilder) generateFixPrompt(root *types.FaultNode) string {
	if len(root.Children) == 0 {
		return "Review the full error output and fix all issues."
	}

	var sb strings.Builder
	sb.WriteString("Fix the following diagnosed build failures:\n\n")

	for i, child := range root.Children {
		sb.WriteString(fmt.Sprintf("%d. **%s** (%s):\n", i+1, child.Message, child.Category))

		switch child.Category {
		case types.FaultMissingDep:
			sb.WriteString("   - Run `go mod tidy` to resolve missing dependencies\n")
			for _, dep := range child.Children {
				sb.WriteString(fmt.Sprintf("   - Add module providing: %s\n", dep.Message))
			}
		case types.FaultImportError:
			sb.WriteString("   - Check import paths match the module structure\n")
			for _, imp := range child.Children {
				sb.WriteString(fmt.Sprintf("   - Fix: %s in %s\n", imp.Message, imp.File))
			}
		case types.FaultCompilation:
			sb.WriteString("   - Fix compilation errors:\n")
			for _, ce := range child.Children {
				if ce.File != "" {
					sb.WriteString(fmt.Sprintf("   - %s:%d — %s\n", ce.File, ce.Line, ce.Message))
				} else {
					sb.WriteString(fmt.Sprintf("   - %s\n", ce.Message))
				}
			}
		case types.FaultTestAssert:
			sb.WriteString("   - Fix failing tests:\n")
			for _, tf := range child.Children {
				sb.WriteString(fmt.Sprintf("   - %s\n", tf.Message))
			}
		case types.FaultTimeout:
			sb.WriteString("   - Investigate timeout: check for infinite loops, slow operations, or insufficient timeouts\n")
		case types.FaultVetError:
			sb.WriteString("   - Fix go vet warnings:\n")
			for _, ve := range child.Children {
				sb.WriteString(fmt.Sprintf("   - %s:%d — %s\n", ve.File, ve.Line, ve.Message))
			}
		}
		sb.WriteString("\n")
	}

	sb.WriteString("Address these issues in priority order (dependencies first, then compilation, then tests).\n")
	return sb.String()
}

// CountFaults returns the total number of leaf fault nodes in the tree.
func CountFaults(node *types.FaultNode) int {
	if node == nil {
		return 0
	}
	if len(node.Children) == 0 {
		return 1
	}
	count := 0
	for _, child := range node.Children {
		count += CountFaults(child)
	}
	return count
}
