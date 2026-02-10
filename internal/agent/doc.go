// Package agent provides abstractions for code review backends (agents).
//
// The package defines the core interfaces and implementations for
// different code review backends: Codex, Claude, and Gemini.
//
// # Architecture
//
// The package is built around three main interfaces:
//
//  1. Agent - executes code reviews and summarizations via CLI subprocesses
//  2. ReviewParser - streams findings from agent review output
//  3. SummaryParser - parses complete summarization output
//
// Agents and parsers are created via the registry in factory.go.
// Adding a new backend requires implementing all three interfaces
// and registering them in the factory.
//
// # Agent Interface
//
// Agents are responsible for:
//   - Checking CLI availability (IsAvailable)
//   - Executing reviews (ExecuteReview)
//   - Executing summarizations (ExecuteSummary)
//   - Returning ExecutionResult for streaming output and lifecycle management
//
// Example usage:
//
//	ag, _ := agent.NewAgent("claude")
//	if err := ag.IsAvailable(); err != nil {
//	    log.Fatal(err)
//	}
//
//	config := &agent.ReviewConfig{
//	    BaseRef:    "main",
//	    Timeout:    5 * time.Minute,
//	    ReviewerID: 1,
//	}
//
//	result, err := ag.ExecuteReview(ctx, config)
//	if err != nil {
//	    log.Fatal(err)
//	}
//	defer result.Close()
//
// # ReviewParser Interface
//
// Parsers stream findings one at a time from agent output:
//
//	parser, _ := agent.NewReviewParser("claude", reviewerID)
//	scanner := bufio.NewScanner(result)
//	agent.ConfigureScanner(scanner)
//
//	for {
//	    finding, err := parser.ReadFinding(scanner)
//	    if agent.IsRecoverable(err) {
//	        continue // skip bad lines
//	    }
//	    if finding == nil {
//	        break // end of stream
//	    }
//	    // process finding
//	}
//
// # Implementations
//
// CodexAgent / CodexOutputParser / CodexSummaryParser: Uses the codex CLI with JSONL output
// ClaudeAgent / ClaudeOutputParser / ClaudeSummaryParser: Uses the claude CLI with plain text / JSON schema output
// GeminiAgent / GeminiOutputParser / GeminiSummaryParser: Uses the gemini CLI with JSON output
package agent
