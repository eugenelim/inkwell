#!/usr/bin/env bash
set -euo pipefail
# Regenerates docs/SECURITY_TESTS.md from // SECURITY-MAP: annotations.
# Run: make security-map (or bash scripts/gen-security-map.sh)
cd "$(git rev-parse --show-toplevel)"
echo "# Security Tests Index"
echo ""
echo "Generated from \`// SECURITY-MAP:\` annotations in test files."
echo "Run \`bash scripts/gen-security-map.sh\` to regenerate."
echo ""
grep -rn "SECURITY-MAP:" --include="*_test.go" . | sort | while read -r line; do
    # format: ./path/to/file.go:42:// SECURITY-MAP: V8.1.1
    echo "- $line"
done
