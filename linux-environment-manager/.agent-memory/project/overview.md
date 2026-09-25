---
id: project/overview
type: project
title: "LEM project overview"
description: >-
  Self-hosted Linux preference, save, telemetry and device-management system
  with a Go server, Go agent and Svelte dashboard.
tags: [architecture, overview]
source: other
created: 2026-09-25
updated: 2026-09-25
status: active
---

LEM is a self-hosted system for a central Go/SQLite server, a per-device Go agent, and a Svelte dashboard. The intended product is focused on small explicit preference files, game saves, telemetry, inventory, notifications and optional Docker management — not full-machine snapshots.

The architecture and product constraints are documented in the repository `ARCHITECTURE.md`, `SECURITY.md`, `COLLECTION-CONTRACT.md` and `AGENTS.md`.
