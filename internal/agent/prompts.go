package agent

import "strings"

// RenderPrompt replaces the {{guidance}} placeholder in a prompt template.
// If guidance is empty, the placeholder is stripped, producing clean output.
// If guidance is non-empty, the placeholder is replaced with an "Additional context:" section.
// If the template contains no placeholder, it is returned unchanged.
func RenderPrompt(template, guidance string) string {
	if guidance == "" {
		return strings.ReplaceAll(template, "{{guidance}}", "")
	}
	return strings.ReplaceAll(template, "{{guidance}}", "\n\nAdditional context:\n"+guidance)
}

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

Output format: file:line: description
{{guidance}}`

// DefaultAntigravityPrompt is the default review prompt for Antigravity CLI.
const DefaultAntigravityPrompt = `Review this git diff for bugs.

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

Output format: file:line: description
{{guidance}}`

// DefaultClaudeRefFilePrompt is the review prompt used when the diff is passed via
// a reference file instead of being embedded in the prompt. This avoids "prompt too long"
// errors for large diffs by having Claude read the diff using its file tools.
const DefaultClaudeRefFilePrompt = `Review this git diff for bugs.

The diff to review is in file: %s
Use the Read tool to examine it.

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

Output format: file:line: description
{{guidance}}`

// DefaultAntigravityRefFilePrompt is the review prompt used when the diff is
// passed via a reference file instead of being embedded in the prompt.
const DefaultAntigravityRefFilePrompt = `Review this git diff for bugs.

The diff to review is in file: %s
Read the file contents to examine the changes.

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

Output format: file:line: description
{{guidance}}`

// DefaultCodexPrompt is the default review prompt for Codex-based agents.
// Used when guidance is provided and we fall back to diff-based review
// because codex's --base flag and stdin prompt (-) are mutually exclusive (#170).
const DefaultCodexPrompt = `Review this git diff for bugs.

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

Output format: file:line: description
{{guidance}}`

// DefaultCodexRefFilePrompt is the review prompt used when the diff is passed via
// a reference file instead of being embedded in the prompt.
const DefaultCodexRefFilePrompt = `Review this git diff for bugs.

The diff to review is in file: %s
Read the file contents to examine the changes.

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

Output format: file:line: description
{{guidance}}`
