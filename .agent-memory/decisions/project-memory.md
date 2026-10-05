---
id: decisions/project-memory
type: decision
title: "Use repository memory instead of a remote memory MCP"
description: >-
  Durable SyncWin decisions are stored in reviewable .agent-memory markdown; MCP
  is reserved for explicit external integrations.
tags: [memory, workflow, opencode]
source: other
created: 2026-09-25
updated: 2026-09-25
status: active
---

## Decision

Use `.agent-memory/` as the cross-session source of truth for this repository. The active OpenCode `openpencil` MCP is a design-file tool, not a memory system, and no dedicated memory MCP is configured.

## Why

Repository markdown is reviewable, portable across agents and checkouts, does not require sending project context to a remote service, and can be committed with the project. OpenCode session history can supplement it but is not the canonical project memory.

## How to apply

At the end of meaningful work, update the relevant context/decision/session memory files and keep secrets out of them. Use an MCP only when a concrete external integration is needed.
