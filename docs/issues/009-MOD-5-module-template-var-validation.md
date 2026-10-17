<!--
SPDX-License-Identifier: MPL-2.0
This Source Code Form is subject to the terms of the Mozilla Public
License, v. 2.0. If a copy of the MPL was not distributed with this
file, You can obtain one at https://mozilla.org/MPL/2.0/.
-->


# MOD-5: Module Template Variable Extraction & Validation

## Overview
Module environment overrides and volume mount paths use Mustache-like template variables (`{{server.id}}`, `{{modules.<name>.<field>}}`, etc.). Previously, unresolved or malformed variables were replaced silently or passed through with partial syntax without validation or diagnostic warnings.

## Changes Made
- Implemented `ExtractTemplateVariables(input string) []string` to extract and deduplicate template variable paths while trimming whitespace.
- Implemented `HasUnresolvedVariables(input string) bool` to test for unresolved placeholders.
- Implemented `ValidateTemplate(input string) ([]string, error)` to detect unbalanced delimiters, stray brackets, empty tags, and validate variable prefixes against known roots (`server`, `config`, `host`, `module`, `modules`).
- Implemented `ValidateTemplateVariables(input string, ctx *Context) ([]string, error)` to check resolution against supplied context and report unresolved variables.
- Normalized template variable whitespace in `Substitute` to allow flexible whitespace syntax (e.g. `{{ server.port }}`).
- Wired diagnostic warnings into `internal/docker/module.go` for unresolved variables in module environment variables and volume paths.
- Added unit tests in `internal/alias/alias_test.go`.
