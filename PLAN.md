# Plan

## 1. Scaffolding (done)

Same setup as super-atlas, sideload only.

- **Layout:** `server/` (Go, chi, SQLite), `app/` (bare React Native + Expo
  modules, committed `android/`), `release/`, `flake.nix`, `module.nix`.
- **Sign-in:** atlas's email-code flow, with a cap on accounts
  (`supercalendar-admin`).
- **Storage:** server side. The app caches what it last fetched.
- **Distribution:** Android sideload; the `distribution` flavor dimension stays
  so Play can be added later.
- **Ports:** dev server on 8090 (atlas uses 8080).

Still to do from scaffolding: run on an emulator/phone, `gen-keystore.sh`,
first `app-v0.1.0` release.

---

## 2. The product

Three kinds of calendar, all shared the same way, plus one main view that
overlays any of them.

| Kind | What's in it | Special powers |
|---|---|---|
| **General** | Events (timed, all-day, recurring) | `.ics` import (files, links, share sheet) |
| **Training** | Sessions with an activity type, plan and result | Copy a week onto another week, then tweak |
| **Cycle** | One entry per day: flow (none → heavy), notes | Period detection and next-period prediction |

- **Sharing:** any member invites by email. The invitee approves it in the
  **Inbox** screen. Every member has the same rights, as asked: edit
  everything, invite, and leave. Nobody "owns" a calendar after it's made.
- **Main calendar (Overview):** the home screen. It overlays the calendars you
  pick, with switches per type, per calendar, and per layer within a type.
- **Switching modes:** one list (a drawer): Overview on top, then the calendars
  grouped by type, then "+ New calendar" and Inbox.

---

## 3. Shared foundation

### Data

```sql
calendars (
  id TEXT PRIMARY KEY,
  kind TEXT NOT NULL CHECK (kind IN ('general','training','cycle')),
  name TEXT NOT NULL,
  color TEXT NOT NULL,              -- default colour for everyone
  settings TEXT NOT NULL DEFAULT '{}',  -- per-kind JSON (week start, plan start, …)
  created_by TEXT REFERENCES users(id) ON DELETE SET NULL,
  created_at, updated_at
)

-- Membership plus each member's own view of the calendar.
calendar_members (
  calendar_id REFERENCES calendars ON DELETE CASCADE,
  user_id     REFERENCES users ON DELETE CASCADE,
  color TEXT,                       -- personal override, NULL = calendar's
  in_overview INTEGER NOT NULL DEFAULT 1,
  position INTEGER NOT NULL,        -- order in the switcher
  joined_at,
  PRIMARY KEY (calendar_id, user_id)
)

calendar_invites (
  id, calendar_id REFERENCES calendars ON DELETE CASCADE,
  inviter_id REFERENCES users ON DELETE SET NULL,
  email TEXT NOT NULL,              -- normalized; may not have an account yet
  status TEXT CHECK (status IN ('pending','accepted','declined','cancelled')),
  created_at, responded_at
)
-- one pending invite per (calendar, email)
CREATE UNIQUE INDEX ... ON calendar_invites(calendar_id, email) WHERE status = 'pending';
```

Rules:

- **Access check:** every `/calendars/{id}/…` route goes through middleware that
  loads the membership. A non-member gets a **404**, so they can't tell the
  calendar exists. This is the most important part to cover with tests.
- **Matching invites to people:** invites match on email, so one sent before
  the person has an account appears in their Inbox after they sign up.
- **Invite email:** sent with the existing mailer: "Ana invited you to *Our
  plans* (training) on Super Calendar", with a link to the latest release APK
  and "sign in with this email".
- **Invites and the account cap:** an email with a pending invite may sign up
  past the cap, because an invite counts as being vouched for. *(Open
  question 4.)*
- **Leaving:** the last member to leave deletes the calendar and everything in
  it. Deleting your account counts as leaving everything.
- **Conflicts:** last write wins (`updated_at`, `updated_by`), which is fine
  for a handful of people. Entries show "edited by X".

### API

```
GET    /calendars                       my calendars (+ my colour, in_overview, position)
POST   /calendars                       {kind, name, color}
PATCH  /calendars/{id}                  name, colour, settings (shared)
PATCH  /calendars/{id}/me               my colour, in_overview, position
DELETE /calendars/{id}/me               leave
GET    /calendars/{id}/members
POST   /calendars/{id}/invites          {email}
DELETE /calendars/{id}/invites/{iid}    cancel a pending one
GET    /invites                         my Inbox (pending, with calendar name/kind, inviter)
POST   /invites/{iid}/accept | /decline
```

### App

- **Switcher drawer:** Overview, then General / Training / Cycle sections, then
  "+ New calendar" (pick a type, name, colour), with an Inbox badge.
- **Inbox:** pending invites showing calendar name, type icon and who sent it,
  with Accept / Decline. The badge count refreshes when the app comes to the
  foreground. Push notifications come later (see §9).
- **Calendar settings:** name, shared colour, my colour, members, invite by
  email, pending invites I've sent (with cancel), and Leave.

---

## 4. General calendar

### Data

```sql
events (
  id, calendar_id REFERENCES calendars ON DELETE CASCADE,
  uid TEXT NOT NULL,                -- iCalendar UID; generated for manual events
  recurrence_id TEXT,               -- set on an overridden occurrence (ICS RECURRENCE-ID)
  title, description, location, url,
  all_day INTEGER NOT NULL,
  start TEXT NOT NULL,              -- all-day: 'YYYY-MM-DD'; timed: RFC3339 UTC
  end   TEXT NOT NULL,              -- exclusive, same format
  tzid TEXT,                        -- original zone, so recurrences keep wall-clock time across DST
  rrule TEXT, exdates TEXT,         -- RFC 5545 RRULE, JSON list
  source TEXT NOT NULL DEFAULT 'manual',  -- 'manual' | 'ics'
  created_by, updated_by, created_at, updated_at,
  UNIQUE (calendar_id, uid, recurrence_id)
)
```

- **Recurrence** is expanded on the server for a date range
  (`github.com/teambition/rrule-go`). The app never expands RRULEs itself.
- `GET /calendars/{id}/events?from&to` returns occurrences in that range.
  Editing a recurring event offers "this one" (writes an override row) or
  "all" (edits the series).

### ICS import

The server parses `.ics` with `github.com/arran4/golang-ical`. The app only
gets the raw text or a URL to the server.

```
POST /calendars/{id}/import/preview   {ics} or {url}  -> events found, duplicates, errors
POST /calendars/{id}/import           same body       -> imported / updated / skipped counts
```

- **Duplicates:** imports dedupe on `(calendar, UID, RECURRENCE-ID)`.
  Re-importing an updated file updates the events instead of duplicating them.
- **Fetching by URL:** the server only fetches `http(s)`/`webcal(s)` URLs, with a
  size limit and a timeout, and refuses private and loopback addresses (to
  avoid SSRF).

Ways in, since Android has no drag-and-drop from websites:

1. **Tapping an `.ics` link or `webcal://` link** in a browser or email.
   Add intent filters for `VIEW` with `text/calendar` / `application/ics` and
   the `webcal`/`webcals` schemes.
2. **Share sheet:** "Share → Super Calendar" for `.ics` files, or text
   containing a link (`SEND` filters). A small hand-written `ShareIntentModule.kt`
   hands the intent to JS, as atlas's native modules do.
3. **"Import file"** in the calendar menu, using the system file picker.

All three lead to the same flow: pick the general calendar, preview the
events, then import.

Later: **subscribing** to a feed URL so the server re-fetches it on a
schedule, and an **export feed** (a secret `/feeds/{token}.ics`) so Google or
Apple Calendar can show a shared calendar. *(Open question 5.)*

### App

- **Views:** month grid, plus an agenda list for the selected day or week.
- **Event editor:** title, all-day, start/end, repeat (common presets plus
  "custom"), location, notes.

---

## 5. Training calendar

### Data

```sql
activity_types (                    -- the calendar's activity library
  id, calendar_id REFERENCES calendars ON DELETE CASCADE,
  name, color, icon,
  fields TEXT NOT NULL DEFAULT '[]', -- which plan fields this type uses (duration, distance, …)
  archived INTEGER NOT NULL DEFAULT 0
)

training_sessions (
  id, calendar_id REFERENCES calendars ON DELETE CASCADE,
  date TEXT NOT NULL,               -- 'YYYY-MM-DD'
  time TEXT,                        -- optional 'HH:MM'
  position INTEGER NOT NULL,        -- order within the day
  activity_type_id REFERENCES activity_types ON DELETE SET NULL,
  title TEXT NOT NULL,
  plan TEXT NOT NULL DEFAULT '{}',  -- {duration_min, distance_m, intensity, blocks:[…]}
  notes TEXT,
  status TEXT NOT NULL DEFAULT 'planned' CHECK (status IN ('planned','done','skipped')),
  result TEXT,                      -- what actually happened: same shape as plan + notes
  created_by, updated_by, created_at, updated_at
)
```

- **Where planning lives:** in the activity library plus `plan`. Each type
  says which fields it uses, so a run asks for distance and pace and a lift
  asks for blocks of sets. `plan` is JSON so the shape can change without
  migrations. The server validates its keys.
- **No recurrence here.** Copying weeks replaces it, and every copy can be
  edited on its own.

### Copying weeks

```
POST /calendars/{id}/training/copy
  {from: '2026-12-07', to: ['2026-12-14', '2026-12-21'], days: 7, mode: 'merge'|'replace'}
  -> {created: [ids], removed: [ids]}
```

- Copies every session in `[from, from+days)` to each target, keeping weekday,
  time and order. Status resets to `planned` and results are cleared.
- `days: 1` copies a single day, and several targets repeat a week for a whole
  block.
- `replace` clears the target weeks first. It all runs in one transaction.
- **Undo:** the response lists what was created and removed, so the app can
  undo straight away.
- **Week numbering:** the calendar's `settings` carry `week_start` (Monday by
  default) and an optional `plan_start`, so weeks show as "Week 1, Week 2…" of
  the plan instead of dates. *(Open question 2.)*

### App

- **Week view (default):** weeks stacked vertically, with each day's sessions
  as cards showing the activity colour and a status tick.
- **Week header menu:** Copy to next week · Copy to… · Clear week.
- **Day actions:** copy day, plus drag to reorder within a day.
- **Session editor:** pick the activity type, fill in that type's plan fields,
  and mark done or skipped with the actual result.
- **Activity types screen:** add, edit and archive types.
- **Month view:** also available, showing one dot per session.

---

## 6. Cycle calendar

### Data

```sql
cycle_days (
  calendar_id REFERENCES calendars ON DELETE CASCADE,
  date TEXT NOT NULL,
  flow TEXT NOT NULL CHECK (flow IN ('none','spotting','light','medium','heavy')),
  notes TEXT,
  updated_by, updated_at,
  PRIMARY KEY (calendar_id, date)
)
```

- **Blank by default:** a day only gets a row when someone logs it, so
  everything else stays empty.
- **Logging:** tapping a day opens a five-step flow picker plus notes.
  "Started today" is a single tap.
- **Sharing:** works like the other calendars (invite by email, approve in
  Inbox), and every member can log.
- **Privacy:** this is the most sensitive data the app holds. It lives only on
  your own server and goes when the calendar or account is deleted. Invite
  emails name the calendar but contain none of its data.

### Detecting periods (from the logged days)

- A **period** is a run of days with flow light, medium or heavy, allowing a
  one-day gap. Spotting can sit inside a period but never starts one.
- **Period start** is the first day of a run, and **period length** is how many
  days the run covers.
- **Cycle length** is the number of days from one period start to the next.

### Prediction

This is a calendar-based method, the standard approach when you only have
period dates. It lives in a pure Go package (`internal/cycle`) with
table-driven tests.

1. **Clean the history:** drop cycles shorter than 15 or longer than 60 days
   from the statistics, since they're almost always a missed or doubled log.
   They still show in the history, marked.
2. **Enough data?**
   - Fewer than **2** usable cycles (3 logged starts): no prediction. Show
     "Log 2 more periods to see predictions".
   - 2–5 cycles: predict using all of them, with a wide range.
   - 6 or more: use the **last 6–12**, weighting recent cycles more (linear
     weights), so a shift over time shows up quickly.
3. **Next start:** last start + the weighted mean cycle length, rounded.
4. **Range:** ± the standard deviation of cycle length, at least 1 day and at
   most 7.
   - Variation over 7–9 days counts as irregular under FIGO's definition. In
     that case the screen says "cycles vary a lot, so this is a rough guess".
5. **Period length:** the median of logged period lengths, used to draw the
   predicted days.
6. **How far ahead:** the next 3 cycles, with the range widening each cycle
   (×√n), so the Overview can show the coming months.
7. **Late:** if today is past the predicted start plus the range and nothing
   has been logged, show "N days late".
8. **Optional, off by default (open question 3):** an estimated ovulation day
   (next start − 14, since the luteal phase is the more stable half of the
   cycle) and a fertile window (ovulation − 5 to ovulation + 1). Labelled
   *estimate, not contraception*.
9. **Backtest:** replay a history one cycle at a time and report the mean
   absolute error in days. It runs as a test against synthetic data and can be
   run as `supercalendar-admin cycle-backtest <calendar>` on real data, to tune
   the window and weights with evidence instead of guessing.

```
GET /calendars/{id}/cycle/days?from&to
PUT /calendars/{id}/cycle/days/{date}       {flow, notes}   (flow 'none' + no notes deletes)
GET /calendars/{id}/cycle/summary           periods, cycle stats, predictions, late
```

### App

- **Month grid:** logged days are tinted by flow, and predicted days are
  outlined or hatched.
- **Summary card:** next period (date ± range, or "not enough data"), average
  cycle and period length, irregular flag.
- **History:** a list of past cycles and their lengths.

---

## 7. Overview (the main calendar)

### What it shows

```
Layers
  [x] General              (type switch)
      [x] Family           (per calendar)
      [ ] Work imports
  [x] Training
      [x] Our plan     [x] planned  [x] done
  [x] Cycle
      [x] Ana          [x] logged period  [x] predicted  [ ] fertile window
```

- **Saving the switches:** per user, on the server, so they follow you to a
  new phone. Per-calendar switches are `calendar_members.in_overview`. Type
  switches and per-type layers go in a `user_settings` table as JSON.
- **New calendars** start switched on.

### API

The overlay is one request, so the app doesn't fan out per calendar:

```
GET /overview?from&to
-> items: [{calendarId, kind, layer, date|start/end, allDay, title, color}]
```

- **Server side:** each kind supplies an *overlay adapter* that turns its rows
  in a range into these items. Events are expanded recurrences; training
  sessions report as `planned` or `done`; cycle reports `period`,
  `predicted` and `fertile`.
- **Adding kinds:** a new calendar kind only needs a new adapter.

### Rendering

- **Day cells:** general and training items are coloured dots or short bars.
  Cycle days are a soft tint behind the cell, so they never crowd out the
  dots.
- **Day agenda:** tapping a day opens a merged agenda for it, and tapping an
  item opens it in its own calendar.

---

## 8. Build order

**Status (2026-11-20):** phases 1–5 are built: server with tests
(`go test ./...`), app typechecks, bundles and builds
(`assembleSideloadDebug`). Not yet run on a phone or emulator. Left: phase 6,
plus drag-to-reorder sessions (sessions keep the order they were added in).

Each phase ends usable and released (`app-v0.x`).

1. **Shared foundation:** calendars, members, invites, Inbox, switcher, calendar
   settings. Membership-check tests: non-member gets 404 everywhere, the last
   member leaving deletes the calendar, account deletion leaves everything,
   and an invite before sign-up appears after it.
2. **General + Overview skeleton:** events CRUD with recurrence, month and
   agenda views, the event editor. Build `/overview` and the Layers sheet now
   with one adapter, so later kinds just plug in.
3. **ICS import:** server parser, preview and dedupe, file picker, then the
   Android `VIEW`/`SEND` intents and webcal links.
4. **Training:** activity types, sessions, week view, copy week/day with undo,
   and its overview adapter.
5. **Cycle:** day logging, period detection, `internal/cycle` prediction with
   tests and backtest, summary UI, and its overview adapter.
6. **Polish:** live updates when someone else edits (a socket like atlas's
   notification stream), invite and late-period notifications, ICS
   subscriptions and export feeds.

## 9. Choices to make with them

- **App libraries:** no date library: `app/src/dates.ts` has the few helpers
  needed, on `YYYY-MM-DD` strings. The month grid and week view
  are hand-built: the overlays are custom anyway, and calendar libraries fight
  that.
- **Offline:** online-first with the last fetch cached. Editing offline waits
  until it's actually missed.
- **Server updates:** the app refreshes on focus and pull-to-refresh; live
  sync comes in phase 6.

## 10. Open questions: defaults picked while building

Each of these has a working default now; say if you want it changed.

1. **Activity fields:** each activity type picks from duration, distance,
   intensity (easy / moderate / hard / max) and exercises (sets × reps × kg).
   New training calendars start with Run, Ride, Swim, Strength, Mobility and
   Rest, so non-workout days fit too.
2. **Training weeks:** calendar dates by default; an optional plan start date
   numbers them "Week 1, Week 2…". One plan per calendar (make another
   calendar for another block).
3. **Cycle:** the fertile window estimate exists but is **off** by default (a
   switch in the cycle screen's menu and the Overview layers). Only flow and
   notes are tracked; no symptoms or mood yet.
4. **Invites and the account cap:** yes, a pending invite lets that email sign
   up past the cap.
5. **ICS:** one-time import (file, link, share sheet, webcal links) with
   updates on re-import. Live subscriptions and export feeds wait for phase 6.
