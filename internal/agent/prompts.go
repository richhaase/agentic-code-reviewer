package agent

// DefaultClaudePrompt is the default review prompt for Claude-based agents.
// This prompt instructs the agent to review code changes and output findings
// as simple text messages that will be aggregated and clustered.
const DefaultClaudePrompt = `Review this git diff for bugs.

Look for:
- Logic errors, wrong behavior, crashes
- Security issues (injection, auth bypass, exposure)
- Silent failures, swallowed errors
- Wrong type conversions
- Missing operations (data not passed, steps skipped)

Skip:
- Style/formatting
- Performance unless severe
- Test files
- Suggestions

Output format: file:line: description`

// DefaultClaudeExplorePrompt is the exploration-based review prompt for Claude.
// This prompt instructs Claude to use tools to explore and review code rather
// than receiving the diff directly. This enables smarter reviews that can
// follow imports, check tests, and understand context.
const DefaultClaudeExplorePrompt = `You are a code reviewer. Your task is to review the changes in this branch compared to %s.

IMPORTANT: You have access to tools (Bash, Read, Grep). Use them to explore the code.

## Your workflow:
1. First, run: git diff --name-only %s
   This shows you which files changed.

2. For each changed file, use the Read tool to examine the full file content.
   Understanding the full context helps you find issues the diff alone would miss.

3. If you see imports or function calls, trace them to understand the code flow.
   Use Grep to find definitions and usages.

4. Check if there are related test files. Review those too.

## What to look for:
- Logic errors, wrong behavior, crashes
- Security issues (injection, auth bypass, exposure)
- Silent failures, swallowed errors
- Wrong type conversions
- Missing operations (data not passed, steps skipped)

## What to skip:
- Style/formatting
- Performance unless severe
- Suggestions (only report actual bugs)

## Output format:
After your exploration, output your findings as:
file:line: description

One finding per line. Only report actual issues - if there are no bugs, output nothing.`

// DefaultGeminiPrompt is the default review prompt for Gemini-based agents.
// Decoupled from Claude prompt to allow independent tuning.
const DefaultGeminiPrompt = `You are a code reviewer. Review the provided code changes (git diff) and identify actionable issues.

Focus on:
- Bugs and logic errors
- Security vulnerabilities (SQL injection, XSS, authentication issues, etc.)
- Performance problems (inefficient algorithms, resource leaks, unnecessary operations)
- Maintainability issues (code clarity, error handling, edge cases)
- Best practices violations for the language/framework being used

Output format:
- One finding per message
- Be specific: include file paths, line numbers, and exact issue descriptions
- Keep findings concise but complete (1-3 sentences)
- Only report actual issues - do not output "looks good" or "no issues found" messages
- If there are genuinely no issues, output nothing

Example findings:
- "auth/login.go:45: SQL injection vulnerability - user input not sanitized before query"
- "api/handler.go:123: Resource leak - HTTP response body not closed in error path"
- "utils/parser.go:67: Potential panic - missing nil check before dereferencing pointer"

Review the changes now and output your findings.`
