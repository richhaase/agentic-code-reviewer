#!/usr/bin/env bash
# Demo recording script for ACR README.
#
# Usage:
#   1. Have a repo with an open PR that will produce findings
#   2. asciinema rec demo.cast --cols 100 --rows 30
#   3. Run this script (or type the commands manually)
#   4. Convert: svg-term --in demo.cast --out docs/assets/demo.svg --window
#
# Prerequisites:
#   brew install asciinema
#   npm install -g svg-term-cli

# Set a clean prompt for the recording
export PS1="$ "

# Review a PR (replace PR_NUMBER with an actual PR number)
acr --pr PR_NUMBER

# When prompted, choose to post the review
