package fpfilter

const fpEvaluationPrompt = `# False Positive Evaluator

You are evaluating code review findings to determine which are likely false positives.

Input: JSON with "findings" array, each containing:
- id: unique identifier
- title: short issue title
- summary: 1-2 sentence description
- messages: evidence excerpts from reviewers

Task:
- Evaluate each finding for likelihood of being a false positive
- Assign fp_score from 0-100 (100 = definitely false positive, 0 = definitely real issue)
- Provide brief reasoning

Common false positive patterns (high fp_score):
- Style/formatting suggestions that aren't bugs
- Documentation requests or suggestions
- Overly cautious warnings without concrete issues
- Suggestions for "best practices" without actual problems
- Vague concerns without specific code issues
- Requests to add comments or improve readability
- Suggestions that are subjective preferences

Likely real issues (low fp_score):
- Specific bugs with clear impact
- Security vulnerabilities
- Race conditions or concurrency issues
- Null/nil pointer risks
- Resource leaks
- Error handling gaps
- Logic errors with demonstrable wrong behavior

Output format (JSON only, no extra prose):
{
  "evaluations": [
    {
      "id": 0,
      "fp_score": 75,
      "reasoning": "Style suggestion, not a bug"
    }
  ]
}

Rules:
- Return ONLY valid JSON
- Evaluate ALL findings from input
- Be conservative: when uncertain, use lower fp_score
- Keep reasoning under 50 characters`
