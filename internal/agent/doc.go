// Package agent provides abstractions for code review backends (agents).
//
// The agent package defines the core interfaces and implementations for
// different code review backends like Codex, Claude, Gemini, etc.
//
// # Architecture
//
// The package is built around two main interfaces:
//
//  1. Agent - represents a code review backend that can execute reviews
//  2. OutputParser - parses backend-specific output into domain.Finding structs
//
// # Agent Interface
//
// Agents are responsible for:
//   - Checking availability (IsAvailable)
//   - Executing reviews (Execute)
//   - Returning output streams for parsing
//
// Example usage:
//
//	agent := agent.NewCodexAgent()
//	if err := agent.IsAvailable(); err != nil {
//	    log.Fatal(err)
//	}
//
//	config := &agent.AgentConfig{
//	    BaseRef: "main",
//	    Timeout: 5 * time.Minute,
//	    ReviewerID: "reviewer-1",
//	}
//
//	reader, err := agent.Execute(ctx, config)
//	if err != nil {
//	    log.Fatal(err)
//	}
//	defer reader.(io.Closer).Close()
//
// # OutputParser Interface
//
// Parsers are responsible for:
//   - Reading from agent output streams
//   - Converting backend-specific formats to domain.Finding
//   - Handling parsing errors gracefully
//
// Example usage:
//
//	parser := agent.NewCodexOutputParser(reviewerID)
//	defer parser.Close()
//
//	scanner := bufio.NewScanner(reader)
//	agent.ConfigureScanner(scanner)
//
//	for {
//	    finding, err := parser.ReadFinding(scanner)
//	    if err != nil {
//	        log.Printf("parse error: %v", err)
//	        continue
//	    }
//	    if finding == nil {
//	        break // end of stream
//	    }
//	    // process finding
//	}
//
// # Current Implementations
//
// CodexAgent: Executes reviews using the codex CLI
// CodexOutputParser: Parses JSONL output from codex
//
// # Future Implementations
//
// The package is designed to support additional backends:
//   - ClaudeAgent (using claude CLI)
//   - GeminiAgent (using gemini CLI)
//   - Custom implementations via Agent interface
package agent
