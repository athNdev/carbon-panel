package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeFile(t *testing.T, root, rel, content string) {
	t.Helper()
	p := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", p, err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", p, err)
	}
}

func baseBundle(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	writeFile(t, root, "src/main.go", "package main\n")
	writeFile(t, root, "knowledge/sections.yaml", "architecture: System structure and seams.\n")
	writeFile(t, root, "knowledge/overview.md", `---
type: Overview
title: Overview
description: Entry point for the knowledge base.
status: stable
owners: [oss-runtime]
---
See [runtime](architecture/runtime.md).
`)
	writeFile(t, root, "knowledge/architecture/runtime.md", `---
type: Architecture
title: Runtime model
description: How the daemon starts and which components it owns.
status: stable
owners: [oss-runtime]
code_refs:
  - src/main.go
---
The runtime starts here.
`)
	return root
}

func hasSubstring(values []string, want string) bool {
	for _, value := range values {
		if strings.Contains(value, want) {
			return true
		}
	}
	return false
}

func TestCleanBundle(t *testing.T) {
	root := baseBundle(t)

	fixed := lint(root, true)
	if len(fixed.Errors) != 0 {
		t.Fatalf("fix reported errors: %v", fixed.Errors)
	}
	if fixed.IndexesWritten == 0 {
		t.Fatal("expected index files to be written")
	}

	clean := lint(root, false)
	if len(clean.Errors) != 0 {
		t.Fatalf("clean bundle reported errors: %v", clean.Errors)
	}
	if clean.Concepts != 2 {
		t.Fatalf("expected 2 concepts, got %d", clean.Concepts)
	}
}

func TestMissingType(t *testing.T) {
	root := baseBundle(t)
	writeFile(t, root, "knowledge/overview.md", `---
title: Overview
description: Entry point.
status: stable
owners: [oss-runtime]
---
No type here.
`)
	r := lint(root, true)
	if !hasSubstring(r.Errors, "type is required") {
		t.Fatalf("expected a type error, got %v", r.Errors)
	}
}

func TestInvariantRequiresHumanVerification(t *testing.T) {
	root := baseBundle(t)
	writeFile(t, root, "knowledge/invariants/data-safety.md", `---
type: Invariant
title: Data safety
description: Worlds survive cleanup.
status: stable
owners: [oss-runtime]
code_refs: [src/main.go]
---
Worlds survive cleanup.
`)
	r := lint(root, true)
	if !hasSubstring(r.Errors, "verified is required") {
		t.Fatalf("expected a verification error, got %v", r.Errors)
	}
}

func TestInvariantRejectsAgentVerification(t *testing.T) {
	root := baseBundle(t)
	writeFile(t, root, "knowledge/invariants/data-safety.md", `---
type: Invariant
title: Data safety
description: Worlds survive cleanup.
status: stable
owners: [oss-runtime]
code_refs: [src/main.go]
verified: { by: agent/zed, at: 2026-10-23 }
---
Worlds survive cleanup.
`)
	r := lint(root, true)
	if !hasSubstring(r.Errors, "verified.by must start with human:") {
		t.Fatalf("expected a human verification error, got %v", r.Errors)
	}
}

func TestDraftInvariantAllowed(t *testing.T) {
	root := baseBundle(t)
	writeFile(t, root, "knowledge/invariants/data-safety.md", `---
type: Invariant
title: Data safety
description: Worlds survive cleanup.
status: draft
owners: [oss-runtime]
code_refs: [src/main.go]
---
Worlds survive cleanup.
`)
	r := lint(root, true)
	if len(r.Errors) != 0 {
		t.Fatalf("draft invariant reported errors: %v", r.Errors)
	}
	if !hasSubstring(r.Warnings, "draft Invariant is not human-verified") {
		t.Fatalf("expected a draft verification warning, got %v", r.Warnings)
	}
}

func TestMissingCodeRef(t *testing.T) {
	root := baseBundle(t)
	writeFile(t, root, "knowledge/architecture/runtime.md", `---
type: Architecture
title: Runtime model
description: How the daemon starts.
status: stable
owners: [oss-runtime]
code_refs: [internal/does-not-exist]
---
Missing.
`)
	r := lint(root, true)
	if !hasSubstring(r.Errors, "code_refs \"internal/does-not-exist\"") {
		t.Fatalf("expected a code_refs error, got %v", r.Errors)
	}
}

func TestBrokenLink(t *testing.T) {
	root := baseBundle(t)
	writeFile(t, root, "knowledge/overview.md", `---
type: Overview
title: Overview
description: Entry point.
status: stable
owners: [oss-runtime]
---
See [missing](architecture/missing.md).
`)
	r := lint(root, true)
	if !hasSubstring(r.Errors, "does not resolve") {
		t.Fatalf("expected a link error, got %v", r.Errors)
	}
}

func TestForbiddenContent(t *testing.T) {
	root := baseBundle(t)
	writeFile(t, root, "knowledge/overview.md", `---
type: Overview
title: Overview
description: Entry point.
status: stable
owners: [oss-runtime]
---
Deployment lives at https://192.168.1.10/admin.
`)
	r := lint(root, true)
	if !hasSubstring(r.Errors, "forbidden content: private network URL") {
		t.Fatalf("expected a forbidden content error, got %v", r.Errors)
	}
}

func TestIndexFix(t *testing.T) {
	root := baseBundle(t)

	before := lint(root, false)
	if !hasSubstring(before.Errors, "index.md is missing or out of date") {
		t.Fatalf("expected a stale index error, got %v", before.Errors)
	}

	if r := lint(root, true); len(r.Errors) != 0 {
		t.Fatalf("fix reported errors: %v", r.Errors)
	}
	if r := lint(root, false); len(r.Errors) != 0 {
		t.Fatalf("bundle still reports errors after fix: %v", r.Errors)
	}
}

func TestReviewAfterWarning(t *testing.T) {
	root := baseBundle(t)
	writeFile(t, root, "knowledge/overview.md", `---
type: Overview
title: Overview
description: Entry point.
status: stable
owners: [oss-runtime]
review_after: 2020-01-01
---
See [runtime](architecture/runtime.md).
`)
	r := lint(root, true)
	if !hasSubstring(r.Warnings, "has passed") {
		t.Fatalf("expected a review warning, got %v", r.Warnings)
	}
}

func TestDuplicateTitleWarning(t *testing.T) {
	root := baseBundle(t)
	writeFile(t, root, "knowledge/architecture/second.md", `---
type: Architecture
title: Runtime model
description: Another concept with the same title.
status: stable
owners: [oss-runtime]
code_refs: [src/main.go]
---
Duplicate title.
`)
	r := lint(root, true)
	if !hasSubstring(r.Warnings, "duplicate title") {
		t.Fatalf("expected a duplicate title warning, got %v", r.Warnings)
	}
}

func TestQueriesPass(t *testing.T) {
	root := baseBundle(t)
	writeFile(t, root, "knowledge/queries.yaml", "- query: runtime model\n  expect: architecture/runtime\n")
	r := lint(root, true)
	if len(r.Errors) != 0 {
		t.Fatalf("expected a passing query, got %v", r.Errors)
	}
	if r.QueriesChecked != 1 || r.QueriesPassed != 1 {
		t.Fatalf("expected 1/1 queries, got %d/%d", r.QueriesPassed, r.QueriesChecked)
	}
}

func TestUnknownQueryExpect(t *testing.T) {
	root := baseBundle(t)
	writeFile(t, root, "knowledge/queries.yaml", "- query: runtime\n  expect: does/not-exist\n")
	r := lint(root, true)
	if !hasSubstring(r.Errors, "does not exist") {
		t.Fatalf("expected an unknown concept error, got %v", r.Errors)
	}
}
