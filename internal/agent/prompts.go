package agent

import "strings"

func RenderPrompt(template, guidance string) string {
	if guidance == "" {
		return strings.ReplaceAll(template, "{{guidance}}", "")
	}
	return strings.ReplaceAll(template, "{{guidance}}", "\n\nAdditional context:\n"+guidance)
}

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

Review the changes now and output your findings.
{{guidance}}`

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

const DefaultGeminiRefFilePrompt = `You are a code reviewer. Review the code changes in the diff file and identify actionable issues.

The diff to review is in file: %s
Read the file contents to examine the changes.

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

Review the changes now and output your findings.
{{guidance}}`

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
