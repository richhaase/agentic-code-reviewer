package feedback

const summarizePrompt = `You are analyzing a GitHub PR to summarize feedback about code review findings.

## Input
You will receive:
- PR description
- All comments and replies from the PR

## Task
Summarize any feedback that indicates code review issues have been:
- Dismissed as false positives or not applicable
- Explained as intentional design decisions
- Acknowledged but deferred to future work
- Marked as resolved

Focus on feedback relevant to code quality findings (bugs, security issues, error handling, etc.).
Ignore unrelated discussion (feature questions, deployment, CI status, etc.).

## Output
Write a concise prose summary (2-5 sentences). Focus on what a code reviewer should probably ignore based on prior discussion.

If no relevant feedback exists, respond with exactly:
No prior feedback on code review findings.

Example outputs:

"The PR description notes this is a prototype and comprehensive error handling will be added in a follow-up PR. A reviewer's concern about the unchecked error in auth.go was addressed - the author explained errors are validated by middleware upstream."

"The author acknowledged the SQL query could use parameterized queries but noted this is an internal admin tool with no user input. A thread about missing nil checks was resolved."

"No prior feedback on code review findings."`
