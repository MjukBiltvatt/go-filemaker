# Migrating from v3 to v4

v4 is a deliberate, breaking redesign. There is no compatibility shim — call
sites change. This guide maps every v3 construct to its v4 equivalent and calls
out the behavior changes worth checking.

The v3 line stays available on its own branch/tag; this guide is for moving a
codebase forward.

## What changed, and why

- **Client-object design.** Records are now plain, immutable data. Instead of
  `record.Commit()` / `record.Delete()`, you pass data to methods on the client:
  `c.Create(...)`, `c.Update(...)`, `c.Delete(...)`. The record no longer holds a
  back-pointer to the session.
- **Goroutine-safety.** A single `*Client` is safe to share across goroutines.
- **`context.Context` everywhere.** Every network method takes a `ctx` as its
  first argument.
- **Declarative finds.** The chained `NewFindCommand().Limit().Sort()` builders
  are replaced by plain struct literals.
- **Lazy authentication.** `New` no longer logs in; the session is established on
  first use.

## Module path & import

Bump the import path to `/v4`:

```go
// v3
import "github.com/MjukBiltvatt/go-filemaker/v3"

// v4
import "github.com/MjukBiltvatt/go-filemaker/v4"
```

```sh
go get github.com/MjukBiltvatt/go-filemaker/v4
```

The package name is `filemaker` in both versions.

## At a glance

| v3 | v4 |
| --- | --- |
| `fm, err := filemaker.New(host, db, user, pass)` | same call, but **no network yet** — login is lazy |
| `defer fm.Destroy()` | `defer c.Logout(ctx)` |
| `fm.Find(layout, NewFindCommand(NewFindRequest(NewFindCriterion("F", "v")).Omit()).Limit(10).Sort("F", filemaker.SortAscending))` | `c.Find(ctx, layout, []filemaker.FindRequest{{Criteria: filemaker.Criteria{"F": "v"}, Omit: true}}, filemaker.WithLimit(10), filemaker.WithSort(filemaker.Asc("F")))` |
| `rec := fm.NewRecord(layout); rec.Set("F", "v"); rec.Commit()` | `c.Create(ctx, layout, filemaker.FieldData{"F": "v"})` |
| `rec.Set("F", "v"); rec.Commit()` (existing record) | `c.Update(ctx, rec, filemaker.FieldData{"F": "v"})` |
| `rec.Delete()` | `c.Delete(ctx, rec)` |
| `rec.CommitToContainer("F", "f.pdf", buf)` | `c.UploadToContainer(ctx, rec, "F", "f.pdf", buf)` |
| `rec.CommitFileToContainer("F", path)` | open the file yourself → `c.UploadToContainer(ctx, rec, "F", name, f)` |
| `rec.ID` (field) | `rec.ID()` (method) |
| `rec.Reset()` | _removed_ — records are immutable; nothing to revert |
| `rec.Map(&v, time.Local)` | `rec.Decode(&v)` — no location arg; **not recursive** |

## Step by step

### Sessions: `New` / `Destroy` → `New` / `Logout`

`New` keeps the same signature but is now **pure** — it does no network I/O and
its error is cheap argument validation only. The session is established on the
first operation that needs it (or eagerly with `c.Authenticate(ctx)`).

```go
// v3 — New logs in; a bad credential fails here.
fm, err := filemaker.New(host, db, user, pass)
if err != nil { /* could be a failed login */ }
defer fm.Destroy()

// v4 — New never logs in; a bad credential surfaces on the first call below.
c, err := filemaker.New(host, db, user, pass)
if err != nil { /* validation only */ }
defer c.Logout(ctx)
```

`Logout` (renamed from `Destroy`) takes a `ctx` and ends the session, but the
client stays usable — a later operation re-authenticates. `WithTimeout` and
`LastActivity()` carry over unchanged.

### Finds: builders → declarative structs

The `NewFindCommand` / `NewFindRequest` / `NewFindCriterion` builders and their
chained `.Limit()` / `.Offset()` / `.Sort()` / `.Omit()` methods are gone. A find
is now a slice of `FindRequest` literals plus options:

```go
// v3
records, err := fm.Find("People", filemaker.NewFindCommand(
    filemaker.NewFindRequest(
        filemaker.NewFindCriterion("Firstname", "Mark"),
        filemaker.NewFindCriterion("Age", "*"),
    ),
    filemaker.NewFindRequest(
        filemaker.NewFindCriterion("Lastname", "==Johnson"),
    ).Omit(),
).Limit(10).Sort("Firstname", filemaker.SortAscending))

// v4
res, err := c.Find(ctx, "People",
    []filemaker.FindRequest{
        {Criteria: filemaker.Criteria{"Firstname": "Mark", "Age": "*"}},
        {Criteria: filemaker.Criteria{"Lastname": "==Johnson"}, Omit: true},
    },
    filemaker.WithLimit(10),
    filemaker.WithSort(filemaker.Asc("Firstname")),
)
records := res.Records
```

- Criteria within one request are AND-ed; separate requests are OR-ed — same
  semantics as v3, expressed as map keys and slice elements.
- `.Omit()` → the `Omit: true` field on the request.
- `.Limit(n)` → `WithLimit(n)`, `.Offset(n)` → `WithOffset(n)`, `.Sort(f, o)` →
  `WithSort(Asc(f))` / `WithSort(Desc(f))`. The underlying `SortRule` struct and
  `SortAscending` / `SortDescending` constants are still exported for dynamic use.
- `Find` now returns a `FindResponse` (with `Records` and a `DataInfo` of host
  counts), not a bare `[]Record`. Use `res.Records`.

### Creating records

`NewRecord` + `Set` + `Commit` collapses into a single `Create`:

```go
// v3
rec := fm.NewRecord("People")
rec.Set("Firstname", "Mark")
err := rec.Commit()
id := rec.ID // populated after commit

// v4
created, err := c.Create(ctx, "People", filemaker.FieldData{"Firstname": "Mark"})
id := created.RecordID
```

`Create` returns the host's acknowledgement (`RecordID`, `ModID`), **not** a
populated record. If you need the full record afterward, issue a `Find` or `Get`.

### Updating records

There are no staged changes. Build a `FieldData` patch and pass it to `Update`
(when you hold a record from a find) or `UpdateByID` (when you hold only an id):

```go
// v3
rec.Set("Firstname", "Mark II")
err := rec.Commit()

// v4 — from a find, you hold the record:
_, err := c.Update(ctx, rec, filemaker.FieldData{"Firstname": "Mark II"})

// v4 — from a create, you hold only an id:
_, err := c.UpdateByID(ctx, "People", created.RecordID, filemaker.FieldData{"Firstname": "Mark II"})
```

`rec.Reset()` has no equivalent and is not needed: a record is immutable, so
there are never uncommitted changes to revert. The patch you pass to `Update` is
the only thing sent.

### Deleting records

```go
// v3
err := rec.Delete()

// v4
_, err := c.Delete(ctx, rec)            // from a find
_, err := c.DeleteByID(ctx, "People", id) // from an id
```

### Container fields

`CommitToContainer` becomes `UploadToContainer`, taking an `io.Reader`:

```go
// v3
err := rec.CommitToContainer("Photo", "photo.jpg", buf)

// v4
err := c.UploadToContainer(ctx, rec, "Photo", "photo.jpg", buf)
// or, by id:
err := c.UploadToContainerByID(ctx, "People", id, "Photo", "photo.jpg", buf)
```

`CommitFileToContainer` (which read a path for you) has no direct equivalent —
open the file and pass it as the reader:

```go
f, err := os.Open("/path/to/photo.jpg")
if err != nil { /* … */ }
defer f.Close()
err = c.UploadToContainer(ctx, rec, "Photo", "photo.jpg", f)
```

v4 also adds `DownloadFromContainer` / `DownloadFromContainerByURL`, which v3 did
not provide.

### Reading values & `Map` → `Decode`

The common getters carry over: `String`/`StringE`, `Int`/`IntE`,
`Int64`/`Int64E`, `Float64`/`Float64E`, `Bool`, `Time`/`TimeE`, and `Get`. Two
changes:

- **Sized integer/float accessors were removed.** `Int8`, `Int16`, `Int32`, and
  `Float32` (and their `…E` forms) are gone — FileMaker numbers come back as
  `float64`, so the sizes added surface without value. Use `Int`, `Int64`, or
  `Float64`.
- **`record.ID` is now `rec.ID()`** (a method, like all record fields).

`Map` is renamed `Decode` and its contract changed:

```go
// v3 — takes a location; recurses into nested structs.
err := rec.Map(&hero, time.Local)

// v4 — no location arg (uses the client's WithLocation); flat only.
err := rec.Decode(&hero)
```

**`Decode` is not recursive** (the most important behavior change to verify). v3
descended into nested structs automatically; v4 maps only the `fm`-tagged fields
of the struct you pass. Decode nested structs explicitly:

```go
type Customer struct {
    Name    string  `fm:"Name"`
    Address Address // not populated by the outer Decode
}

err := rec.Decode(&customer)
err = rec.Decode(&customer.Address) // decode the nested struct yourself
```

The time zone that was the second argument to `Map` is now a client-level setting
(`WithLocation`, default UTC), stamped onto every record. Override per call with
`rec.TimeIn(field, loc)`.

## Behavior changes to watch

- **Login timing.** A bad host/credential failed at `New` in v3; in v4 it
  surfaces on the first operation (or on an explicit `Authenticate(ctx)`).
- **`Decode` is flat**, not recursive (see above).
- **`Find` returns `FindResponse`**, not `[]Record`. Reach for `res.Records`.
- **Default find limit.** v3 imposed a default limit of 100 client-side; v4 sends
  no limit unless you set `WithLimit`, so the server's own default (also 100)
  applies. Same effective result — but if you relied on exactly 100, set it
  explicitly.
- **Removed accessors:** `Int8`/`Int16`/`Int32`/`Float32`, `record.Reset`,
  `CommitFileToContainer`. Replacements are above.

## New in v4

Beyond the migration, v4 adds capabilities v3 lacked: `Get`/`GetByID` and
`GetRange`, `Duplicate`, script execution (`RunScript` and `WithScript` on any
operation), database/layout/script metadata, `SetGlobalFields`, container
downloads, opt-in optimistic concurrency (`IfUnchanged`/`WithModID`), the
`Bool`/`Date`/`Timestamp`/`Time`/`Duration` write-value wrappers, ISO date I/O
(`WithDateFormat`), and opt-in token re-authentication. See the
[README](../README.md) and the [package reference](https://pkg.go.dev/github.com/MjukBiltvatt/go-filemaker/v4)
for these.
