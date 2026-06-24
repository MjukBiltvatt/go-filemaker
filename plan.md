# go-filemaker v4 Refactor Plan

A breaking redesign of the library targeting a new major version (`/v4`). The
work is happening on `feature/v4-dev`.

## Goals

1. **Client object-based design.** Records become plain data carriers. Instead
   of `record.Commit()`, `record.Delete()`, etc., the data is passed to methods
   on a client (`client.Save(record)`, `client.Delete(...)`). This removes the
   back-pointer from records to the session.
2. **Goroutine-safety.** A single client must be safe to share across
   goroutines: concurrent finds, creates, and updates with no data races.
3. **More accessible types.** Move away from the map-based, method-chained
   builders (`NewFindCommand().Limit().Sort()`) toward declarative, exported
   struct types that can be written as plain literals.

## Naming convention

Go struct field names are **idiomatic and self-documenting; the JSON tag carries
the wire name.** Applied consistently across all types:

- Diverge from the wire where Go reads better: `recordId` → `ID`, `modId` →
  `ModID`, the message text → `Message.Text`, the find result slice → `Records`
  (tagged `json:"data"`).
- Keep the wire name where it is already descriptive: `FieldData`, `PortalData`.

This rejects strict wire-mirroring (which would force `Data`, `RecordID`,
`Message`, …) in favor of readable call sites; the JSON tags preserve the exact
wire mapping for (un)marshaling and debugging.

---

## Current architecture (v3)

- `session.go` — `Session` holds `HttpClient`, `Token`, `Host`, `Database`,
  `Username`, `Password`, and an unexported `lastActivity`. It exposes `New`,
  `Destroy`, `Find`, `NewRecord`, `LastActivity`.
- `record.go` — `Record` holds `ID`, `Layout`, `StagedChanges`, `FieldData` and
  a `*Session` back-pointer. Mutating operations (`Commit`, `Create`, `Delete`,
  `CommitToContainer`) live here and call `r.Session.HttpClient.Do(...)`. Typed
  getters (`String`, `Int`, …, `Map`) also live here.
- `findcommand.go` / `findrequest.go` / `findcriterion.go` — builders backed by
  `map[string]interface{}` with chained mutator methods.
- `errors.go` — three sentinel errors.

### Problems to fix along the way

- **Data race on `lastActivity`.** `Find`/`Destroy` write `s.lastActivity`
  without synchronization while other goroutines may read via `LastActivity()`.
- **Session copy bug.** `Find` calls `newRecord(layout, r, *s)`, which copies the
  `Session` by value and stores `&copy` on each record. Token refresh or any
  later session change would never reach already-returned records.
- **Massive HTTP boilerplate.** Every method repeats: build request, send, read
  body, unmarshal, check `Messages[0].Code`. This is ~7 near-identical blocks.
- **Unchecked errors / panics.** `http.NewRequest` errors are ignored (the next
  line dereferences `req`), and `jsonRes.Messages[0]` is indexed without a
  length check — a malformed/empty response panics.
- **HTTP status largely ignored.** Only the FileMaker message code is inspected;
  transport-level non-2xx responses are not surfaced cleanly.
- **Deprecated APIs.** `io/ioutil` is still imported in `record.go`.
- **No `context.Context`.** No cancellation/deadline support per request.
- **Map-based find builders** expose `interface{}` everywhere and rely on
  mutation + chaining rather than plain values.

---

## Target architecture (v4)

### 1. `Client` (replaces `Session`)

```go
type Client struct {
    httpClient *http.Client
    host       string
    database   string
    username   string
    password   string

    mu           sync.RWMutex
    token        string
    lastActivity time.Time
}

func New(host, database, username, password string, opts ...Option) (*Client, error)
func (c *Client) Destroy(ctx context.Context) error
func (c *Client) LastActivity() time.Time
```

- All exported fields become unexported; the client is manipulated only through
  methods. This is what makes locking enforceable.
- `token` and `lastActivity` are guarded by `mu` (decided: `sync.RWMutex`, not
  atomics — see Concurrency model). Reads use `RLock`; token refresh and the
  activity stamp use `Lock` so both fields update as one unit and a
  check-then-reauth sequence can hold the lock across the whole critical
  section.
- `*http.Client` is already safe for concurrent use; share it directly.
- Options (`func(*config)`): `WithTimeout` (carried over from v3); the two opt-in
  reauth triggers `WithReauthOnInvalidToken` (reactive: re-auth + retry on a 952)
  and `WithReauthOnIdle(timeout ...)` (proactive: refresh before a request when
  idle past the timeout, default `DefaultIdleTimeout`) — both funnel into one
  de-duplicated reauth, see the internal HTTP layer; and `WithLocation` (time
  zone for date/timestamp fields, stamped onto returned records; default UTC).

### 2. Record operations move onto the client

Each method returns a **dedicated response type** that mirrors exactly what the
Data API populates for that verb — so any field the caller can reach is
guaranteed to have a value. The library does **not** issue a follow-up `GET` to
backfill data the API didn't return; if the caller wants the full record after a
write, they issue their own `Find`.

```go
func (c *Client) Find(ctx context.Context, layout string, q Query) (FindResponse, error)
func (c *Client) Create(ctx context.Context, layout string, fields FieldData) (CreateResponse, error)
func (c *Client) Update(ctx context.Context, layout, id string, fields FieldData, opts ...UpdateOption) (UpdateResponse, error)
func (c *Client) Delete(ctx context.Context, layout, id string) error
func (c *Client) UploadToContainer(ctx context.Context, layout, id, field, filename string, data io.Reader) error
```

`FieldData` is an exported `map[string]any` holding the fields to write.
`Create`/`Update` marshal it **faithfully** — no value coercion. Values should
be strings or numbers; for Go ergonomics there are opt-in marshalable wrappers
(`Bool` → 1/0, `Date` → MM/DD/YYYY, `Timestamp` → MM/DD/YYYY HH:MM:SS; zero time
→ "" clears the field) that slot directly into the map and need no `Client`
change. This mirrors the read side's raw-vs-typed split (`Get` vs
`Bool`/`Time`). A bare Go `bool`/`time.Time` is sent as-is (and likely rejected
by the host) — use the wrappers.

Response types, each matching the documented Data API envelope:

```go
// Create response: { "recordId": "147", "modId": "0" }
type CreateResponse struct {
    RecordID string
    ModID    string
}

// Update response: { "modId": "3" }
type UpdateResponse struct {
    ModID string
}

// Delete response: {} — nothing to return, so Delete yields only an error.

// Find response: { "data": [ … ], "dataInfo": { … } }
type FindResponse struct {
    Records  []Record
    DataInfo DataInfo
}

type DataInfo struct {
    Database         string
    Layout           string
    Table            string
    TotalRecordCount int
    FoundCount       int
    ReturnedCount    int
}
```

- A single-record `Get(ctx, layout, id) (Record, error)` is **deferred** for now
  (removed in phase 3). When reintroduced it shares the `Find` decode path and
  would map the host's record-missing code `101` to an `errors.Is`-friendly
  `ErrRecordNotFound` sentinel. `Find` covers lookups in the meantime.
- `DataInfo` is always present on a successful find (FMS 18+) and is the only
  source of `FoundCount` for pagination, so every field stays meaningful.

### 3. `Record` becomes a pure data carrier

```go
type Record struct {
    ID         string
    ModID      string
    Layout     string
    FieldData  map[string]any
    PortalData map[string][]map[string]any // portal name → rows; new in v4
}
```

- **No `*Session` back-pointer, no mutating methods.** This is the core of the
  client-based redesign and removes the copy bug entirely.
- **`ModID` and `PortalData` are new in v4**, populated from the `data[]` items
  the API returns. `ModID` powers optional optimistic-locking on `Update` via the
  `WithModID(modID)` update option: the host rejects the write (code `306`) if
  the record changed since `modID` was read, surfaced as the `errors.Is`-friendly
  `ErrRecordModified` sentinel. No portal accessors are planned for the initial
  cut beyond exposing the raw map.
- Keep the **read-only typed accessors** — pure functions of the data, all value
  receivers. The set is trimmed to what FileMaker actually needs (numbers come
  back as `float64`, so the small int/float sizes were dropped):
  `Get`, `Has`, `String`/`StringE`, `Int`/`IntE`, `Int64`/`Int64E`,
  `Float64`/`Float64E`, `Bool`, and the time accessors. `Has` distinguishes an
  absent field from a present zero value.
- **Time + location.** Date/timestamp fields carry no zone, so the location is a
  client-level concern: `WithLocation(loc)` (default UTC) is stamped onto every
  record `Find` returns. `Time(field)`/`TimeE(field)` use that location;
  `TimeIn(field, loc)`/`TimeInE(field, loc)` override it explicitly.
- **`Decode` (was `Map`).** Renamed — "decode generic map → typed struct" is the
  conventional name (cf. `mapstructure.Decode`), and `Map` reads as a transform
  in Go. `Decode(obj any) error` follows the `json.Unmarshal` model: it errors
  only on structural misuse (not a non-nil pointer to a struct) and is lenient
  per field (missing/empty → zero value). It uses the record's location for time
  fields. **Not recursive** (behavior change from v3): records are flat, so it
  maps only `fm`-tagged fields of the struct passed in; untagged and `fm:"-"`
  fields are left untouched, and nested structs are decoded by calling `Decode`
  on them directly (`rec.Decode(&customer.Address)`). Needs a migration-guide
  callout.
- The container-download helper needs the client (downloading streams an
  authenticated URL), so it is a client method:
  `client.ContainerData(ctx, record, field) ([]byte, error)`.

**Editing model (decided): data-in/data-out.** No in-record staged changes.
Callers build a `FieldData` map and pass it to `Create`/`Update`, which return
the API acknowledgement (`CreateResponse`/`UpdateResponse`), **not** a refreshed
record. Records are immutable from the library's perspective, which is the most
goroutine-friendly option.

### 4. Declarative find types (replaces the builders)

```go
type Query struct {
    Requests []Request
    Sort     []SortRule
    Limit    int // 0 = server default
    Offset   int
}

type Request struct {
    Criteria map[string]string // field name -> find value; AND-ed together
    Omit     bool
}

type SortRule struct {
    Field string
    Order SortOrder // SortAscending / SortDescending
}

type SortOrder string
const (
    SortAscending  SortOrder = "ascend"
    SortDescending SortOrder = "descend"
)
```

- Users write plain literals; no chaining, no `interface{}`:

  ```go
  q := filemaker.Query{
      Requests: []filemaker.Request{
          {Criteria: map[string]string{
              "Firstname": "Mark",
              "Age":       "*",
          }},
          {Criteria: map[string]string{"Lastname": "==Johnson"}, Omit: true},
      },
      Limit: 10,
      Sort:  []filemaker.SortRule{{Field: "Firstname", Order: filemaker.SortAscending}},
  }
  ```

- A custom `MarshalJSON` on `Query` translates these structs into the FileMaker
  Data API wire shape (`{"query":[{"Field":"val","omit":"true"}],"sort":[…],
  "limit":N,"offset":N}`). This isolates the ugly wire format in one place.
- Optional thin constructor helpers (`filemaker.NewQuery(...)`) can be kept for
  ergonomics, but they are no longer the only path.

### 5. Internal HTTP layer (de-duplication)

Add one unexported helper that every operation funnels through:

```go
func (c *Client) do(ctx context.Context, method, url string, body io.Reader, out *responseBody) error
```

Responsibilities, in one place:
- Build the request with `http.NewRequestWithContext`.
- Set headers (`Content-Type`, `Authorization: Bearer <token>` under `RLock`).
- Send, read, and unmarshal the body.
- Check transport status code **and** FileMaker message code safely (guard
  against empty `Messages`).
- Stamp `lastActivity` on success/any host response (under `Lock`).
- Reauth triggers funnel through one de-duplicated `reauthenticate(ctx, used)`:
  `WithReauthOnInvalidToken` handles a 952 with a single re-auth + retry;
  `WithReauthOnIdle` refreshes before the attempt when idle. De-dup uses a
  dedicated `reauthMu` plus a double-check on the *used* token (the token the
  failed/last request carried), so concurrent callers collapse to one auth and
  the network call never holds the read/write lock (reads stay live).

This collapses the seven duplicated blocks and is where correctness fixes land.

### 6. Errors

```go
type Message struct {
    Code int
    Text string
}

type APIError struct {
    Messages []Message
}

func (e *APIError) Error() string // joins all non-OK messages
func (e *APIError) Code() int     // convenience: first message's code
```

**The `messages` array stays internal.** It is the transport/status layer, not
caller-facing data:
- On success it carries only `[{code:"0","OK"}]` — no value to expose, and
  hanging it on the response types would contradict the "every reachable field
  is populated and meaningful" rule. So it is **not** a field on
  `CreateResponse`/`FindResponse`/etc.
- On failure it is the *source* for a typed `*APIError`, surfaced through the
  normal `error` return. Callers use `if err != nil` / `errors.As`, never a
  status field.
- This rejects the "return a result type wrapping the whole response body"
  alternative — that would push envelope-plumbing onto every caller and invite
  them to re-implement error checking the library should own.

Handling rules (fixing v3's `Messages[0]` bug):
- Length-check before indexing; an empty/garbled `messages` array yields a
  decode error, never a panic.
- `messages` is modeled as a slice and `APIError` can hold all of them — no
  dropping entries past the first, since the API types it as an array.
- Common codes get `errors.Is`-friendly sentinels: `401` → no records, `952` →
  invalid token (feeds the reactive `WithReauthOnInvalidToken` retry path).
- Keep/relocate the value-accessor sentinels: `ErrNotNumber`, `ErrNotString`,
  `ErrUnknownFormat`.
- "No records found" (`401`): `Find` returns a `FindResponse` with empty
  `Records` and nil error (preserve current behavior). (A `Get` with an
  `ErrRecordNotFound` sentinel for code `101` is deferred along with the `Get`
  method.)

> Note: `messages` is distinct from script output. If Data API script execution
> is added later, `scriptResult`/`scriptError` arrive inside the `response`
> object and *would* be exposed as real fields on the response types — keeping
> `messages` internal does not preclude that.

### 7. Concurrency model (documented invariant)

- A `*Client` is safe for concurrent use by multiple goroutines.
- A `Record` returned by the library is owned by the caller; concurrent reads
  are safe, concurrent mutation of one record is the caller's responsibility.
- Verify with `go test -race` plus a test that fans out concurrent
  `Find`/`Create`/`Update` against an `httptest` server.

**Primitive (decided): `sync.RWMutex`, not atomics.** Rationale:
- `lastActivity` is a `time.Time` (multiple machine words), so it can't live in
  a single atomic without storing unix-nano `int64` and converting on every
  access — a readability tax for no real benefit.
- Auto-reauth is a check-then-act sequence; a mutex lets the whole "read token →
  re-auth on expiry → store new token" run as one critical section, preventing
  duplicate re-auth. Atomics would force a `CompareAndSwap` loop.
- Contention is negligible: the lock is held only for a token read and a
  post-response field update around an HTTP call, so the lock-free speed edge of
  atomics is never measurable here.

---

## Proposed package layout

```
client.go     // Client + New/Destroy/options/LastActivity + do()/locking
              //   + Find/Create/Update/Delete/ContainerData/UploadToContainer
record.go     // Record data type + FieldData + typed getters + Decode
values.go     // Bool/Date/Timestamp write-value wrappers
find.go       // Query/Request/SortRule/SortOrder + MarshalJSON
errors.go     // APIError + sentinels
doc.go        // package doc / overview example
*_test.go     // httptest-backed tests incl. -race
```

The full `*Client` surface lives in one file rather than being split across
`client.go`/`records.go`: Go files are organizational only, the library is
small, and a `records.go`/`record.go` pair would be easy to confuse. If
`client.go` later grows unwieldy, peeling the transport layer (`do()` + auth +
locking) into an internal `transport.go` is a trivial follow-up since it is all
methods on one type.

---

## API: before → after

```go
// v3
fm, _ := filemaker.New(host, db, user, pass)
rec := fm.NewRecord("Layout")
rec.Set("Name", "Mark")
rec.Commit()
rec.Set("Name", "Mark II")
rec.Commit()
rec.Delete()

// v4
c, _ := filemaker.New(host, db, user, pass)
created, _ := c.Create(ctx, "Layout", filemaker.FieldData{"Name": "Mark"})
_, _ = c.Update(ctx, "Layout", created.RecordID, filemaker.FieldData{"Name": "Mark II"})
_ = c.Delete(ctx, "Layout", created.RecordID)
// Want the full record back after a write? Issue your own read:
found, _ := c.Find(ctx, "Layout", filemaker.Query{ /* … by id/criteria … */ })
_ = found.Records
```

---

## Versioning & migration

- Module path bumped to `github.com/MjukBiltvatt/go-filemaker/v4` and the Go
  directive raised to `go 1.21` (done early, in phase 2's tooling pass).
- This is intentionally breaking — ship as a clean `v4.0.0`.
- Provide a **migration guide** in the README mapping each v3 call to its v4
  equivalent (the table above, expanded). Keep the v3 line available on its own
  branch/tag for existing users.

---

## Implementation phases

- [x] **1. Scaffold types.** Add `Client` (unexported fields), `Record` data
   type, and the declarative `find.go` types with `MarshalJSON` + unit tests for
   the JSON output. No network code yet.
- [x] **2. Internal `do()` helper.** Centralize request/response handling with
   the safety fixes (status checks, `Messages` length guard, context). Landed
   here ahead of schedule: the `sync.RWMutex`-guarded `token`/`lastActivity`
   plus the documented concurrency invariant and the `-race` fan-out test
   `TestConcurrentDo` (from 5), and the `APIError`/`Message` types threaded
   through `do()`/`send()` with the empty-`messages` panic guard (from 6).
- [x] **3. Port operations to the client.** Implement `Find`, `Create`,
   `Update`, `Delete`, container upload/download on top of `do()`. (A
   single-record `Get` is deferred.)
- [x] **4. Port read-side helpers.** Move typed getters + `Decode` (was `Map`)
   onto the data-only `Record`; drop `io/ioutil`. Includes the trimmed getter
   set, `Has`, and `WithLocation` (time zone carried onto records).
- [x] **5. Concurrency hardening.** De-duplicated concurrent re-authentication
   via a dedicated `reauthMu` + used-token double-check (separate from `c.mu`, so
   the auth round-trip never blocks token reads). Both reauth triggers
   (`WithReauthOnInvalidToken` reactive, `WithReauthOnIdle` proactive) funnel
   through it. Covered by a barrier-gated `-race` test asserting N concurrent
   952s collapse to one auth, plus proactive on-idle/while-active tests. The
   `sync.RWMutex`, concurrency invariant, and `-race` fan-out test landed in
   phase 2.
- [ ] **6. Errors — remaining.** Expose `errors.Is`-friendly sentinel(s) for the
   codes callers may branch on (e.g. invalid token) and finish error
   documentation. The `APIError`/`Message` types, the messages length-guard, and
   threading through `do()` already landed in phase 2; the value-accessor
   sentinels (`ErrNotNumber`/`ErrNotString`/`ErrUnknownFormat`) already exist.
- [ ] **7. Docs.** Rewrite README for the v4 API, add migration guide, update the
   README install/import paths to `/v4` (module path itself already bumped).
- [ ] **8. Verify.** `go vet ./...`, `go test -race ./...`, and a manual smoke
   test against a real FileMaker server (creds available locally). Confirm the
   actual date/timestamp format(s) the Data API returns (see Deferred → time
   formats) and the container-upload request shape (the dropped
   `Content-Disposition` header) and the write-value wrappers (`Bool` → number,
   `Date`/`Timestamp` formats).

---

## Deferred (revisit later)

- **Configurable time formats (`WithTimeFormats`).** `TimeInE` currently tries a
  fixed list of layouts until one parses, which silently assumes US `MM/DD`
  ordering and cannot disambiguate `MM/DD` vs `DD/MM` (both parse). The
  principled fix is to treat the accepted layout(s) as client config (like
  `WithLocation`), defaulting to the current list, carried onto records. Held
  until the phase-8 smoke test shows whether the Data API actually emits
  non-US/variable date formats — if it normalizes to a fixed format, the simple
  list stays and we just document the assumption.
- **Runtime-toggleable `autoReauth` (`SetAutoReauth`).** Options are init-only by
  design (immutable config → lock-free reads). `autoReauth` is the one with a
  plausible runtime case (flip off to *detect* an expired token). If needed,
  make the field an `atomic.Bool` with a `SetAutoReauth(bool)` setter —
  lock-free, no involvement of the main `RWMutex`. Held pending a concrete use
  case; trivial to add later without breaking changes. (`WithLocation` and
  `WithTimeout` stay init-only: the file's zone is stable with a `TimeIn`
  override, and per-call timeouts already work via `context` deadlines.)

---

## Settled decisions

1. **Editing model: data-in/data-out.** `Create`/`Update` take a `FieldData`
   map. No mutable staged changes on the record.
5. **Per-method response types.** Each verb returns a dedicated type mirroring
   the Data API envelope (`CreateResponse{RecordID,ModID}`,
   `UpdateResponse{ModID}`, `FindResponse{Records,DataInfo}`; `Delete` returns
   only `error`), so every reachable field is guaranteed populated. Writes do
   **not** auto-issue a follow-up read. `Record` gains `ModID` and `PortalData`.
2. **`context.Context`: yes.** Every network method takes `ctx` as its first
   argument (`Find`, `Create`, `Update`, `Delete`, `Destroy`, container
   upload/download), built via `http.NewRequestWithContext`.
3. **Token auto-refresh: opt-in, two orthogonal triggers.** Off by default.
   `WithReauthOnInvalidToken` (reactive) re-auths + retries once on a 952;
   `WithReauthOnIdle(timeout ...)` (proactive) refreshes before a request when
   idle past the timeout (default `DefaultIdleTimeout`, ~14 min). Composable —
   reactive-only, proactive-only, or both (recommended); both share one
   de-duplicated reauth. Named by trigger for clarity (not `WithAutoReauth`).
4. **Synchronization primitive: `sync.RWMutex`.** Chosen over atomics because
   `lastActivity` is multi-word, the auto-reauth path needs a check-then-act
   critical section, and contention is negligible. (See Concurrency model.)
```
