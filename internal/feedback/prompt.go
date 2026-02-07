package feedback

const summarizePrompt = `You are analyzing a GitHub PR to extract a structured list of code review findings that have been discussed.

## Input
You will receive:
- PR description
- All comments and replies from the PR

## Task
Extract every code review finding that was discussed and categorize each by its resolution:
- DISMISSED: Rejected as false positive, not applicable, or incorrect
- FIXED: Acknowledged and a fix was applied (include commit if mentioned)
- ACKNOWLEDGED: Accepted as valid but deferred to future work
- INTENTIONAL: Explained as an intentional design decision

Focus on findings relevant to code quality (bugs, security issues, error handling, race conditions, resource leaks, etc.).
Ignore unrelated discussion (feature questions, deployment, CI status, etc.).

## Output Format
One finding per line:
- STATUS: "short finding description" -- reason (by @author)

Preserve specific technical details of each finding. Do NOT generalize or combine findings.

If no relevant feedback exists, respond with exactly:
No prior feedback on code review findings.

## Examples

Example 1 (multiple findings discussed):
- DISMISSED: "Non-atomic merge of shared map" -- Map is only accessed under caller's mutex, atomicity not needed (by @alice)
- FIXED: "Unchecked error from db.Connect()" -- Fixed in commit abc123 (by @bob)
- INTENTIONAL: "Graph writes outside SQL transaction" -- Intentional ordering; orphaned graph nodes are harmless, reverse would leave dangling references (by @alice)
- ACKNOWLEDGED: "Missing nil check on user pointer" -- Deferred to follow-up PR #42 (by @carol)

Example 2 (no relevant findings):
No prior feedback on code review findings.`
