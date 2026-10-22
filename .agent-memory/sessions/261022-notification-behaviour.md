---
id: sessions/261022-notification-behaviour
type: session
title: "Notifications: interrupt only for what needs attention"
description: >-
  Opening the dashboard replayed its notification history as a pile of toasts
  and a device that came back online was one of them. The server now says
  which events interrupt, the dashboard decides once when the event arrives,
  and a recovery notice is never swallowed by the anti-flap window.
tags: [notifications, inbox, toast, sse, dispatcher, usability]
source: agent
created: 2026-10-22
updated: 2026-10-22
status: active
---

## The two defects

1. **The dashboard replayed its history as toasts.** The inbox poll ran with
   `since=0` on every open, and every unread event became a toast. A toast
   is for something that is happening now.
2. **A device that came back online was toast-worthy.** "Everything is fine
   again" is history, not an interruption. It still had to be *recorded* — the
   user asked for exactly that.

A third defect only the live check found: the anti-flap window dropped the
"back online" notice when another one had been recorded a few minutes
earlier, so the alert stayed open in the history and the device that
recovered was never reported. The recovery notice is now recognised as the
resolution of an alert that was raised.

## How the behaviour is decided now

- The **server** owns the rule. The event catalogue carries an `interrupt`
  flag, and the effective popup list is derived from it. The dashboard reads
  the list it is given; it does not keep a copy.
- The **dashboard** decides once, when the event arrives: the in-app alerts
  are on, the event is on the list, and it is less than a minute old. The
  toasts live in their own list, so a toast cannot appear and then vanish as
  the event ages.
- The **inbox** keeps everything. The bell shows the unread count; the
  notification centre groups the history by day.

## The rule worth remembering

> An event that is older than a minute never interrupts, whatever its type.

It is what separates "the dashboard is telling me something now" from "the
dashboard is telling me what happened". The first is a toast; the second is
the inbox.

## The trap worth remembering

The full-page screenshot (`captureBeyondViewport`) paints `position: fixed`
elements against the document, not the viewport. A toast that looked cut off
was perfectly inside the window. The geometry was checked with a viewport
capture and `getBoundingClientRect` before anything was "fixed".

## Verification

Against the live stack with the real agent: a device was stopped, the server
noticed five minutes later, and the browser showed exactly one toast. When
the device came back there was no toast, the bell count went up and the inbox
recorded the event. Reopening the dashboard replayed nothing.
