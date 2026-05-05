# Security Tests Index

Generated from `// SECURITY-MAP:` annotations in test files.
Run `bash scripts/gen-security-map.sh` to regenerate.

| File | Line | Test function | ASVS requirements |
| --- | --- | --- | --- |
| `cmd/inkwell/security_test.go` | 12 | `TestLogFileMode` | V8.1.1 V8.2.1 |
| `internal/action/security_test.go` | 11 | `TestActionIDsHaveHighEntropy` | V6.3.1 |
| `internal/compose/security_test.go` | 11 | `TestDraftTempfileMode` | V8.1.1 V8.2.1 |
| `internal/compose/security_test.go` | 28 | `TestEditorCommandUsesArgvNotShell` | V12.1.1 V12.3.1 |
| `internal/graph/security_test.go` | 13 | `TestGraphClientTLSVerificationEnabled` | V9.1.3 |
| `internal/store/security_test.go` | 13 | `TestDatabaseFileMode` | V8.1.1 V8.2.1 |
| `internal/store/security_test.go` | 34 | `TestSearchByPredicateSurvivesAdversarialInput` | V5.3.4 |
| `internal/ui/security_test.go` | 9 | `TestOpenInBrowserUsesArgvNotShell` | V12.1.1 V12.3.1 |

## ASVS Requirement Summary

| Requirement | Description | Tests |
| --- | --- | --- |
| V5.3.4 | SQL injection prevention via parameterised queries | `store.TestSearchByPredicateSurvivesAdversarialInput` |
| V6.3.1 | Cryptographically random identifiers (high entropy) | `action.TestActionIDsHaveHighEntropy` |
| V8.1.1 | Sensitive files created with restricted permissions (0600) | `cmd.TestLogFileMode`, `compose.TestDraftTempfileMode`, `store.TestDatabaseFileMode` |
| V8.2.1 | Sensitive data not world-readable on the filesystem | `cmd.TestLogFileMode`, `compose.TestDraftTempfileMode`, `store.TestDatabaseFileMode` |
| V9.1.3 | TLS certificate verification not disabled | `graph.TestGraphClientTLSVerificationEnabled` |
| V12.1.1 | OS commands use argv form (not shell interpolation) | `compose.TestEditorCommandUsesArgvNotShell`, `ui.TestOpenInBrowserUsesArgvNotShell` |
| V12.3.1 | Shell injection prevention in subprocess invocation | `compose.TestEditorCommandUsesArgvNotShell`, `ui.TestOpenInBrowserUsesArgvNotShell` |
