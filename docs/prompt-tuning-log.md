# Prompt Tuning Log

## Baseline

| Run | Agent | Commit | Findings | False Positives | Notes |
|-----|-------|--------|----------|-----------------|-------|
| 1 | codex | b2e0e9e | 4 | 1 | baseline; 1 timeout retry |
| 2 | claude | b2e0e9e | 20 | 16 | 80% FP rate; 4 true bugs found |
| 3 | gemini | b2e0e9e | 6 | 3 | 50% FP rate; 1 reviewer failed |

## Tuning Runs

| Run | Agent | Commit | Findings | False Positives | Notes |
|-----|-------|--------|----------|-----------------|-------|
| 4 | claude | c9fc2a8+ | 1 | 0 | v1 tuned prompt; 0% FP but missed 3 TPs |
| 5 | claude | 90d0776+ | 0 | 0 | v2 prompt; LGTM - too restrictive |
| 6 | claude | 90d0776+ | 11 | 8 | v3 prompt; 73% FP; found ReviewerID bug + 2 test failures from our changes |
