---
id: sessions/261018-notifications-center
type: session
title: "Notification center with per-event preferences and a working sound"
description: >-
  Replaced the notification settings modal with an Administration screen: an
  inbox with filters and read state, per-event in-app preferences, and a sound
  picker with volume and preview. Fixed the in-app toggle and sound, which the
  old modal saved but the dashboard never applied.
tags: [notifications, sound, ui, preferences, inbox, sse]
source: agent
created: 2026-10-18
updated: 2026-10-18
status: active
---

## What was asked

The notifications and sound were poorly done, and the operator wanted a
notification center where each user can choose which events they receive.

## What was wrong

- The in-app provider config stored only `sound`. Its `enabled` and `events`
  columns were never applied: the dispatcher inserts the inbox row and
  broadcasts over SSE **before** provider filtering, so disabling "In-site
  alerts" did nothing and every event became a toast.
- The settings modal saved `sound` to the server, but `NotificationToast` read
  `sync-win-notif-sound` from localStorage, which the modal never updated. The
  sound choice therefore never reached the toast.
- Every toast created a new `AudioContext` and never closed it; a context
  created before a user gesture is suspended, so the first sound could be
  silently dropped.
- The bell was a limited unread dropdown, read events disappeared from it, and
  the only settings surface was a 586-line modal. The event list was hardcoded
  in the modal, separate from the server's event types.

## What was built

Worktree `sync-win-notifications`, branch `feat/notifications-center`.

### Server

- `notify.EventCatalog` plus `GET /api/notifications/events` returns `type`,
  `label`, `description` and `severity` for every event. The UI no longer keeps
  a second list.
- The `web_inbox` config gained `volume` (0–1), a `pulse` sound and
  `events_set`. `events_set` separates a deliberate empty selection from a row
  written by the old modal, which always stored `[]` while showing everything.
- The default in-app response has all four events, sound `beep`, volume `0.5`.
- `GET /api/notifications/inbox` accepts `limit` (1–100, default 50).
- `POST /api/notifications/inbox/read` accepts `{"all": true}`, backed by
  `MarkAllNotificationEventsRead`, so "mark all" covers history the browser has
  not loaded.
- Tests: inbox sound/volume validation, catalog coverage, config defaults and
  round trip, invalid values, read-all, session requirements.

### Web

- **`Notifications.svelte`** (Administration → Notifications) with two tabs:
  - Inbox: the latest 100 events, unread/read, event and device filters,
    unread-only, mark one/all read, and a device link.
  - Preferences: in-app enabled, per-event popup selection, beep/chime/pulse/
    silent with volume and a preview, plus the Telegram and webhook forms using
    the server catalog.
- **`lib/notification-sound.ts`** uses one `AudioContext`, a gentle gain curve,
  three sounds and an `unlockNotificationAudio` call from the app's first
  pointer/key gesture.
- **`lib/notification-prefs.ts`** loads and saves the `web_inbox` config as a
  shared store; toasts use `shouldNotify`, so unselected events still enter the
  inbox but do not pop up or play a sound.
- The bell is a five-item preview with a count, "Mark all read" and "Open
  notification center". The user menu's Notifications item navigates there.
- The old `NotificationsModal.svelte` was deleted; its sound test duplicated the
  helper.

### Version

`0.11.0` (feat) with a CHANGELOG section; the release workflow publishes it as
the release body.

## Verification

- Server `go test ./...` green; `golangci-lint --new-from-rev` clean.
- Web: **22 files / 165 tests** (4 new for the center), `svelte-check` 0
  errors, `vite build` clean.
- The screen test covers filters, mark-all with the all flag, the PUT body for
  sound/volume/events, and the fallback when the catalog request fails.
- The e2e fleet sweep now includes Notifications; run it against the dev stack
  after merging.

## Not done

- The bell's "mark all" does not live-update an already-open center (the center
  refreshes on mount/Refresh); a shared read-state store would fix that.
- Only the four existing event types are selectable; adding a server event still
  means adding it to the catalogue.
- Sounds are synthesized Web Audio tones, not audio files; no custom upload.
- E-mail provider and quiet hours are out of scope.
