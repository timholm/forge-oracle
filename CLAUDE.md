# forge-oracle

## Build & Test
- `make build` — compile
- `make test` — run all tests
- `go test ./... -v` — verbose tests

## Architecture
Forge-oracle is a Go library integrated directly into the factory-v2 binary as a new internal package (internal/oracle). It exposes a middleware-style API where each pipeline stage (discover, research, synthesize, build, validate, audit) can be wrapped with oracle hooks. Core components: (1) RetrieverGuard — wraps the discover and research HTTP calls, applies probe-gradient reranking to arXiv results and GitHub search results, detects implicit cross-paper connections via embedding comparison with reasoning-aware similarity, and tags all retrieved context with provenance tiers. Stores embeddings in the existing pgvector-enabled Postgres. (2) WorldModelSimulator — a pre-flight layer before Claude CLI invocations that uses a distilled prompt-to-outcome predictor (backed by a fast Haiku call) to estimate whether the planned build action will succeed; returns a confidence score and suggested prompt rewrites. (3) FaultTreeBuilder — parses build/test failure output into a structured fault tree (AND/OR gates over failure causes), then generates a targeted retry prompt that addresses the diagnosed root cause. (4) TopologyOptimizer — after each pipeline run, logs the full trace (prompts, responses, scores) and runs contrastive analysis to update the agent topology document that controls Opus/Sonnet routing, context window allocation, and turn budgets. (5) ComplexityCalibrator — analyzes the ProductSpec against a complexity budget derived from the product category and outputs a scoping recommendation that the synthesizer incorporates. All components share a common trace log in Postgres for observability and topology refinement.

## Module
github.com/timholm/forge-oracle
