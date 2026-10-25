# Unexecuted mutation ledger

The final campaign has no LIVED mutants. All 29 entries below are reported
NOT COVERED and are classified as **uncovered scope**. They were not executed
by this package-local campaign. None is relabeled equivalent, killed, or a
demonstrated missing assertion. Status-independent full identities and exact
source lines/deltas are in mutants.json; run.json supplies source identity.

| Identity prefix | Location | Delta | Disposition evidence |
| --- | --- | --- | --- |
| 57976e608016d689 | internal/config/policy.go:62:9 | `!=` → `==` | ApplyAdmitted transition, including validation/policy/admission rejection and exclusion retention; current package policy tests exercise Apply. Broader adapter coverage is later expansion. |
| fe362f2a77acbe1b | internal/config/policy.go:65:45 | `!=` → `==` | ApplyAdmitted transition, including validation/policy/admission rejection and exclusion retention; current package policy tests exercise Apply. Broader adapter coverage is later expansion. |
| fd5bb5f07b8b4fc5 | internal/config/policy.go:69:9 | `!=` → `==` | ApplyAdmitted transition, including validation/policy/admission rejection and exclusion retention; current package policy tests exercise Apply. Broader adapter coverage is later expansion. |
| 0247d1a0e8972557 | internal/config/policy.go:73:29 | `>` → `>=` | ApplyAdmitted transition, including validation/policy/admission rejection and exclusion retention; current package policy tests exercise Apply. Broader adapter coverage is later expansion. |
| 688def09962fd02c | internal/config/policy.go:73:29 | `>` → `<=` | ApplyAdmitted transition, including validation/policy/admission rejection and exclusion retention; current package policy tests exercise Apply. Broader adapter coverage is later expansion. |
| 6a78f5eecfffc1f3 | internal/fix/plan.go:197:30 | `!=` → `==` | Directive-state error return before planning; this selected error guard was not executed as a mutant. |
| 07fc3b1e982637de | internal/fix/plan.go:259:172 | `!=` → `==` | Charged sort error return (proposal, edit or protected-span ordering); this selected interruption guard was not executed as a mutant. |
| 85359e96675ed935 | internal/fix/plan.go:301:77 | `!=` → `==` | Budget failure while recording an invalid preservation decision; the selected campaign did not execute this guard mutant. |
| 9d225f27984249ee | internal/fix/plan.go:308:77 | `!=` → `==` | Budget failure while recording an invalid preservation decision; the selected campaign did not execute this guard mutant. |
| 2655792e91fcfa77 | internal/fix/plan.go:333:18 | `!=` → `==` | Gremlins did not execute this switch-case token. TestBoundariesOwnershipPreconditionsAndRules checks invalid-span and precondition outcomes; that ordinary coverage does not prove a kill for this token. |
| 514cb4257281dd87 | internal/fix/plan.go:335:18 | `!=` → `==` | Gremlins did not execute this switch-case token. TestBoundariesOwnershipPreconditionsAndRules checks invalid-span and precondition outcomes; that ordinary coverage does not prove a kill for this token. |
| 0990224998c95b8b | internal/fix/plan.go:433:10 | `!=` → `==` | Charged sort error return (proposal, edit or protected-span ordering); this selected interruption guard was not executed as a mutant. |
| d6610aad357191b0 | internal/fix/plan.go:574:10 | `!=` → `==` | Charged sort error return (proposal, edit or protected-span ordering); this selected interruption guard was not executed as a mutant. |
| aea45db6c5e293c7 | internal/fix/validate.go:326:13 | `==` → `!=` | FileView.Evaluations nil-trace fallback is not exercised in this selected package campaign. |
| 8bc00fb5cf436ae9 | internal/fix/validate.go:428:64 | `!=` → `==` | Specific reserveJSON map reservation/recursive-error guard is unexecuted by this campaign; no equivalence or exhaustion-detection claim. |
| b7007d502c23645d | internal/fix/validate.go:433:50 | `!=` → `==` | Specific reserveJSON map reservation/recursive-error guard is unexecuted by this campaign; no equivalence or exhaustion-detection claim. |
| ddcdb560418e4cbc | internal/fix/validate.go:436:52 | `!=` → `==` | Specific reserveJSON map reservation/recursive-error guard is unexecuted by this campaign; no equivalence or exhaustion-detection claim. |
| 239b139beaa6c29d | internal/fix/validate.go:535:192 | `!=` → `==` | Target sort interruption guard is unexecuted by this campaign. |
| 83982388235f2aea | internal/fix/validate.go:777:30 | `!=` → `==` | Final checkpoint rollback of a changed candidate is unexecuted by this campaign. |
| 3c7a5d39cabb6375 | internal/fix/validate.go:1116:9 | `==` → `!=` | ChangedDependency publication query is outside the exercised fix-package API paths. Consumer/integration campaign expansion is deferred. |
| d0b07782731ccbb6 | internal/fix/validate.go:1116:39 | `==` → `!=` | ChangedDependency publication query is outside the exercised fix-package API paths. Consumer/integration campaign expansion is deferred. |
| ed3e93557c5097d9 | internal/fix/validate.go:1119:98 | `!=` → `==` | ChangedDependency publication query is outside the exercised fix-package API paths. Consumer/integration campaign expansion is deferred. |
| 7904c7d69195a18f | internal/fix/validate.go:1124:19 | `>` → `>=` | ChangedDependency publication query is outside the exercised fix-package API paths. Consumer/integration campaign expansion is deferred. |
| 6c5f33fb463eda26 | internal/fix/validate.go:1124:19 | `>` → `<=` | ChangedDependency publication query is outside the exercised fix-package API paths. Consumer/integration campaign expansion is deferred. |
| c4dd338f67a0e17e | internal/fix/validate.go:1125:28 | `!=` → `==` | ChangedDependency publication query is outside the exercised fix-package API paths. Consumer/integration campaign expansion is deferred. |
| b407a8c73212ae19 | internal/fix/validate.go:1130:70 | `!=` → `==` | ChangedDependency publication query is outside the exercised fix-package API paths. Consumer/integration campaign expansion is deferred. |
| 33c3a5f5b60ddb1e | internal/fix/validate.go:1136:53 | `!=` → `==` | ChangedDependency publication query is outside the exercised fix-package API paths. Consumer/integration campaign expansion is deferred. |
| 469aaccb44337215 | internal/fix/validate.go:1140:66 | `!=` → `==` | ChangedDependency publication query is outside the exercised fix-package API paths. Consumer/integration campaign expansion is deferred. |
| 8fa124d422d0e7bb | internal/fix/validate.go:1144:54 | `!=` → `==` | ChangedDependency publication query is outside the exercised fix-package API paths. Consumer/integration campaign expansion is deferred. |

These gaps remain visible for future targeted or consumer mutation work. No
production semantics were changed and no files/operators were removed after
observing results. No additional Go test was justified by an executed survivor
in this baseline; uncovered error/API paths remain explicit limitations.
