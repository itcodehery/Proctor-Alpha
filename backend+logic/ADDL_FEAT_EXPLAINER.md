# Proctor Backend — New Features Explainer

## Overview

Two features were added to the Go backend: a **student heartbeat / ping system** with automatic offline detection, and an **admin kick endpoint** that permanently removes a student from a room. Both fit into the existing patterns — same mutex discipline, same `broadcastUpdate` calls, same bcrypt admin-key checks.

---

## Feature 1 — Student Ping & Offline Detection

### The problem

`UserSession` already had a `LastPing time.Time` field, but nothing kept it fresh except `SyncCodeHandler`. If a student stopped syncing code (e.g. their network dropped or they closed the tab), the server had no way to distinguish an idle student from a disconnected one. They'd stay `Online` forever.

### What was added

**`POST /student/ping`** (`PingHandler` in `ping_kick.go`)

The student client calls this endpoint on a short interval (e.g. every 15–20 seconds). The handler:

1. Decodes `room_id` and `user_id` from the request body.
2. Acquires the write mutex and finds the matching `UserSession`.
3. Sets `LastPing = time.Now()`.
4. If the student was previously marked `Offline`, flips them back to `Online` and broadcasts a `ROOM_UPDATE` so the examiner dashboard updates immediately.

```
POST /student/ping
{ "room_id": "ABC123", "user_id": "deadbeef01234567" }

→ 200 { "message": "Ping received" }
```

**`startOfflineDetector(sweepInterval, timeout time.Duration)`** (`ping_kick.go`)

Called once from `main()` right after the WebSocket hub is started. It spins up a goroutine with a `time.Ticker`. On every tick it:

1. Acquires the write mutex.
2. Iterates every `Active` room (skips `Waiting`, `Paused`, `Complete` — no point checking those).
3. For each `Online` student, checks whether `time.Now() - LastPing > timeout`.
4. If stale, sets `ActiveStatus = Offline`.
5. If any student in a room changed, broadcasts a `ROOM_UPDATE` for that room.

Recommended values (set in `main()`):

```go
startOfflineDetector(30*time.Second, 45*time.Second)
```

This means the sweep runs every 30 seconds and a student is considered offline if they haven't pinged in the last 45 seconds. Tune to taste — tighter values catch drops faster but are more sensitive to transient network hiccups.

### Integration checklist

- Add `http.HandleFunc("/student/ping", PingHandler)` in `main()`.
- Call `startOfflineDetector(...)` in `main()` after `go wsHub.run()`.
- On the student client, call `POST /student/ping` every ~15 seconds while the exam is active.

---

## Feature 2 — Admin Kick Student

### The problem

`AdminUpdateUserHandler` can change a student's `ActiveStatus` (e.g. mark them `Flagged`), but there was no way to actually *remove* a student from `room.Students`. A disruptive or mistakenly added student would persist in the slice for the lifetime of the room.

### What was added

**`POST /admin/kick-student`** (`KickStudentHandler` in `ping_kick.go`)

The handler:

1. Decodes `room_id`, `admin_key`, and `user_id`.
2. Verifies the admin key with `bcrypt.CompareHashAndPassword` — same check used by every other admin route.
3. Builds a new slice that excludes the target student (a standard Go filter-in-place pattern using a zero-length slice backed by the same array).
4. If the original and filtered lengths are equal, the student wasn't found — returns 404.
5. Broadcasts `ROOM_UPDATE` so the examiner's live view loses the row immediately.
6. Broadcasts `ROOM_LIST_UPDATE` to `"all"` in case any dashboard is showing student counts.
7. Calls `go saveRooms()` to persist the change to `rooms.json`.

```
POST /admin/kick-student
{
  "room_id":   "ABC123",
  "admin_key": "your-secret-key",
  "user_id":   "deadbeef01234567"
}

→ 200 { "message": "Student removed from room" }
→ 401  (bad admin key)
→ 404  (room or student not found)
```

### Why filter-in-place?

```go
filtered := room.Students[:0]           // zero-length, same backing array
for _, s := range room.Students {
    if s.UserID != req.UserID {
        filtered = append(filtered, s)
    }
}
room.Students = filtered
```

This avoids allocating a second slice when most students are kept. It is safe here because the loop reads from `room.Students` (the original range copy) while writing to `filtered` which starts at index 0 — there is no overlap risk.

### Integration checklist

- Add `http.HandleFunc("/admin/kick-student", KickStudentHandler)` in `main()`.
- On the examiner frontend, add a "Remove" button per student row that calls this endpoint.

---

## Files changed

| File | Change |
|---|---|
| `ping_kick.go` | New file — `PingHandler`, `startOfflineDetector`, `KickStudentHandler` |
| `main.go` | Two new `http.HandleFunc` registrations + one `startOfflineDetector` call |

No changes were required to `rooms.go`, `realtime.go`, or any existing handlers. The new code reuses all existing types (`Room`, `UserSession`, `Online`, `Offline`, `broadcastUpdate`, `mu`, `rooms`, `saveRooms`).

---

## Sequence diagrams

### Ping flow

```
Student client          /student/ping          Hub (WebSocket)    Examiner frontend
     |                        |                      |                   |
     |-- POST /student/ping ->|                      |                   |
     |                        | update LastPing       |                   |
     |                        | if was Offline:       |                   |
     |                        |-- broadcastUpdate --->|                   |
     |                        |                       |-- ROOM_UPDATE --->|
     |<-- 200 "Ping received"-|                       |                   |
     |                        |                       |                   |
   [30s later, no ping]       |                       |                   |
     |               startOfflineDetector sweep       |                   |
     |                        | mark Offline          |                   |
     |                        |-- broadcastUpdate --->|                   |
     |                        |                       |-- ROOM_UPDATE --->|
```

### Kick flow

```
Examiner frontend      /admin/kick-student      Hub (WebSocket)    All observers
     |                        |                      |                  |
     |-- POST kick-student -->|                      |                  |
     |                        | bcrypt verify key    |                  |
     |                        | filter Students[]    |                  |
     |                        | saveRooms()          |                  |
     |                        |-- broadcastUpdate(ROOM_UPDATE) -------->|
     |                        |-- broadcastUpdate(ROOM_LIST_UPDATE) --->|
     |<-- 200 "removed" ------|                      |                  |
```
