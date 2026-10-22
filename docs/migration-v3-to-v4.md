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
| `fm, err := filemaker.New(host, db, user, pass)` | `c, err := filemaker.New(host, db, filemaker.BasicAuth(user, pass))` — **no network yet**, login is lazy |
| `*filemaker.Session` | `*filemaker.Client` |
| `defer fm.Destroy()` | `defer c.Logout(ctx)` |
| `fm.Find(layout, NewFindCommand(NewFindRequest(NewFindCriterion("F", "v")).Omit()).Limit(10).Sort("F", filemaker.SortAscending))` | `c.Find(ctx, layout, []filemaker.FindRequest{{Criteria: filemaker.Criteria{"F": "v"}, Omit: true}}, filemaker.WithLimit(10), filemaker.WithSort(filemaker.Asc("F")))` |
| `rec := fm.NewRecord(layout); rec.Set("F", "v"); rec.Commit()` | `c.Create(ctx, layout, filemaker.FieldData{"F": "v"})` |
| `rec.Set("F", "v"); rec.Commit()` (existing record) | `c.Update(ctx, rec, filemaker.FieldData{"F": "v"})` |
| `rec.Set("F", true)` | `filemaker.FieldData{"F": filemaker.Bool(true)}` — a bare `bool` is no longer sent as `1`/`0` |
| `rec.Delete()` | `c.Delete(ctx, rec)` |
| `rec.CommitToContainer("F", "f.pdf", buf)` | `c.UploadToContainer(ctx, rec, "F", "f.pdf", &buf)` |
| `rec.CommitFileToContainer("F", path)` | open the file yourself → `c.UploadToContainer(ctx, rec, "F", name, f)` |
| `rec.GetContainerData("F")` | `c.DownloadFromContainer(ctx, rec, "F")` → `res.Data` |
| `rec.ID` (field) | `rec.ID()` (method) |
| `rec.Layout` / `rec.FieldData` (fields) | `rec.Layout()` / `rec.Fields()` (methods) |
| `rec.String("F")` / `rec.StringE("F")` (and the other getter pairs) | `rec.String("F")` — one getter, returning `(string, error)` |
| `rec.Time("F", loc)` / `rec.TimeE("F", loc)` | `rec.TimeIn("F", loc)`; `rec.Time("F")` uses the client's `WithLocation` |
| `rec.Reset()` | _removed_ — records are immutable; nothing to revert |
| `rec.Map(&v, time.Local)` | `rec.Decode(&v)` plus `WithLocation(time.Local)` on the client — the default is UTC; **not recursive** |
| error text `failed at host: <message> (<code>)` | `*filemaker.APIError` with `Code()`, matched with `errors.Is` / `errors.As` |

## Step by step

### Sessions: `New` / `Destroy` → `New` / `Logout`

`New` takes the account name and password wrapped in `filemaker.BasicAuth`
rather than as two strings, and is now **pure** — it does no network I/O and
its error is cheap argument validation only. The session is established on the
first operation that needs it (or eagerly with `c.Authenticate(ctx)`).

```go
// v3 — New logs in; a bad credential fails here.
fm, err := filemaker.New(host, db, user, pass)
if err != nil { /* could be a failed login */ }
defer fm.Destroy()

// v4 — New never logs in; a bad credential surfaces on the first call below.
c, err := filemaker.New(host, db, filemaker.BasicAuth(user, pass))
if err != nil { /* validation only */ }
defer c.Logout(ctx)
```

`Logout` (renamed from `Destroy`) takes a `ctx` and ends the session, but the
client stays usable — a later operation re-authenticates. `WithTimeout` carries
over unchanged (default 30 seconds).

The session type is now `Client`: replace `*filemaker.Session` with
`*filemaker.Client` wherever you store or pass it. Its exported fields (`Token`,
`Host`, `Database`, `Username`, `Password`, `HttpClient`) are gone; the token is
private, and the client manages its own HTTP client. `ResponseBody` is gone too —
every operation returns a typed `…Response` instead.

`LastActivity()` keeps its signature but not its behavior. v3 set it to the
login time in `New` and moved it only on a successful `Find` and on `Destroy`
(not on `Commit` or `Delete`). v4 returns the zero time until the first request
succeeds, then moves it on every successful request. Code that reconnected after
an idle period — typically `time.Since(fm.LastActivity()) > 15*time.Minute`
followed by a fresh `New` — now sees a zero time on a new client, and the
check fires at once. Replace that pattern with the client options built for it:

```go
c, err := filemaker.New(host, db, filemaker.BasicAuth(user, pass),
    filemaker.WithReauthOnIdle(),         // refresh before the host's idle timeout
    filemaker.WithReauthOnInvalidToken(), // and retry once if a token has expired anyway
)
```

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
  `SortAscending` / `SortDescending` constants are still exported for dynamic use,
  but the constants are now the typed `SortOrder`: an order held in a `string`
  variable becomes `filemaker.SortRule{Field: f, Order: filemaker.SortOrder(o)}`.
- `FindRequest` keeps its name but is now a struct, not a map, so v3 map
  literals such as `filemaker.FindRequest{"F": "v"}` no longer compile. Put the
  criteria in its `Criteria` field. `FindCriterion`, `FindCommand`, and their
  `AddCriterion` / `AddRequest` methods are gone: add keys to the `Criteria` map
  and append to the `[]FindRequest` slice instead.
- Criteria values are strings. v3 took `interface{}`, so a number could be
  passed as is; `Criteria` is a `map[string]string`, so format it first
  (`strconv.Itoa(n)`).
- `Find` now returns a `FindResponse` (with `Records` and a `DataInfo` of host
  counts), not a bare `[]Record`. Use `res.Records`. A find that matches nothing
  still returns an empty `Records` and a nil error, as v3 did.

### Creating records

`NewRecord` + `Set` + `Commit` (or `rec.Create()`) collapses into a single
`Create`:

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

### Writing values

v3's `Set` converted some Go values before sending them; a `FieldData` map does
not convert them the same way. Check every value you used to pass to `Set`:

- **`bool` needs the `Bool` wrapper.** `Set("Active", true)` sent `1`, and
  `false` sent `0`. A bare `bool` in `FieldData` is sent as JSON `true`/`false`,
  which is not FileMaker's 1/0. Wrap it: `filemaker.FieldData{"Active":
  filemaker.Bool(active)}`.
- **Integers are sent as strings of their digits** (see *Behavior changes to
  watch*). v3 converted them to `float64`, which lost digits beyond 2^53.
- **Dates and times need a wrapper.** A `time.Time` in `FieldData` is sent as
  Go's standard JSON text, such as `2024-03-01T09:30:00+01:00`, in v4 as it was
  in v3. FileMaker's date, time and timestamp fields don't accept that form:
  they expect `03/01/2024`, `09:30:00` or `03/01/2024 09:30:00`, and reject the
  `T` separator and the zone suffix. Wrap the value as `filemaker.Date(t)`,
  `filemaker.Timestamp(t)` or `filemaker.Time(t)` to match the field's type, or
  `filemaker.Duration(d)` for an elapsed time. The wrapper writes `t`'s clock
  time in `t`'s own location.

Strings and floats are sent as before.

### Deleting records

```go
// v3
err := rec.Delete()

// v4
_, err := c.Delete(ctx, rec)            // from a find
_, err := c.DeleteByID(ctx, "People", id) // from an id
```

### Container fields

`CommitToContainer` becomes `UploadToContainer`, taking an `io.Reader`. It
returns an `UploadResponse` carrying the record's new mod ID, like `Update`:

```go
// v3 — buf is a bytes.Buffer, passed by value
err := rec.CommitToContainer("Photo", "photo.jpg", buf)

// v4 — pass a pointer: a bytes.Buffer value is not an io.Reader
_, err := c.UploadToContainer(ctx, rec, "Photo", "photo.jpg", &buf)
// or, by id:
_, err := c.UploadToContainerByID(ctx, "People", id, "Photo", "photo.jpg", &buf)
```

`CommitFileToContainer` (which read a path for you) has no direct equivalent —
open the file and pass it as the reader:

```go
f, err := os.Open("/path/to/photo.jpg")
if err != nil { /* … */ }
defer f.Close()
_, err = c.UploadToContainer(ctx, rec, "Photo", "photo.jpg", f)
```

`GetContainerData` becomes `DownloadFromContainer`, which returns a
`DownloadResponse` with the contents in `Data` and the host's media type in
`ContentType`:

```go
// v3
data, err := rec.GetContainerData("Photo")

// v4
res, err := c.DownloadFromContainer(ctx, rec, "Photo")
data := res.Data
```

An empty container is now `ErrEmptyContainer`, where v3 failed with an
"invalid or unsecure streaming URI" error, so a loop over many records can skip
those with `errors.Is`. `DownloadFromContainerByURL` is new: it downloads from a
container URL you already hold, without a record.

### Reading values & `Map` → `Decode`

The common getters carry over by name — `String`, `Int`, `Int64`, `Float64`,
`Bool`, and `Time` — with the changes below. `Get` carries over too, still
returning the raw value alone (now typed `any`, `nil` for an absent field). The
v3 fields `rec.Layout` and `rec.FieldData` are now the methods `rec.Layout()`
and `rec.Fields()`; `Fields` returns a copy, since a record is immutable.

- **Each typed getter returns an error; the `…E` forms are gone.** v3 paired a silent
  getter, which returned the zero value on any failure, with an `…E` form that
  returned an error. v4 has one getter under the plain name, with the `…E`
  form's signature: `rec.Int("Age")` returns `(int, error)`. Replace `IntE` with
  `Int`. Where v3 code used the silent form, handle the error, or discard it
  with `n, _ := rec.Int("Age")` to keep v3's behavior visibly.
- **An absent field is `ErrMissingField`, and an empty field is not an error.**
  Every getter now reads a field the record does not have — typically a typo, or
  a field that is not on the layout — as `ErrMissingField`, where v3 reported a
  type error (`ErrNotNumber`, `ErrNotString`) or `ErrUnknownFormat`. An empty
  field reads as the zero value with a nil error, as it does in `Decode`, where
  v3's `IntE` or `TimeE` reported one. Use `rec.Get(f) == ""` to tell an empty
  field from a stored zero, and `rec.Has(f)` to tell an absent field from an
  empty one. `Bool` now returns an error too, for an absent field only.
- **`String` reads a number field as its text.** v3's `StringE` returned
  `ErrNotString` for a number field, and `String` returned `""`. v4's `String`
  returns the number's exact text, in the notation the host sent (`7.0`, `1e3`),
  and `StringSlice` returns it as one element. Code that used `ErrNotString` to
  tell a number field from a text field should check the type from `Get`
  instead: `filemaker.Number` or `string`.
- **Getter errors name the field and the record.** A failed read reads
  `filemaker: record "9" in layout "People": field "Age": value is not a number`
  rather than the bare sentinel, and still matches it with `errors.Is`.
- **`Time(field, loc)` and `TimeE(field, loc)` are both `TimeIn(field, loc)`.**
  `Time` no longer takes a location: `Time(field)` uses the client's
  `WithLocation`, which defaults to UTC (see below). Rewriting
  `rec.Time(f, time.Local)` as `rec.Time(f)` compiles but reads the value as UTC
  unless the client sets `WithLocation(time.Local)`.
- **Sized integer/float accessors were removed.** `Int8`, `Int16`, `Int32`, and
  `Float32` (and their `…E` forms) are gone — the sizes added surface without
  value. Use `Int`, `Int64`, or `Float64`, or the new `Number` for the exact
  digits.
- **Numbers are exact.** Number fields come back as `filemaker.Number`, the
  host's exact decimal text, instead of `float64`: `Int64` on a 17-digit ID
  returns that ID rather than a neighbour of it. See *Behavior changes to watch*.
- **`record.ID` is now `rec.ID()`** (a method, like all record fields).
- **Portal rows read through the same getters.** v3 had no portal support. In
  v4, `rec.Portal("Lines")` returns the portal's rows as `PortalRow` values with
  `String`, `Int`, …, `Decode`, and the row's `ID()`/`ModID()`, keyed by
  qualified field names (`"Lines::Qty"`). Write rows with `WithPortalData`,
  each a `PortalRowData` carrying the row's `ID` (and `ModID` to lock the edit).

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

Leave the nested field untagged, as above. Putting an `fm` tag on it now returns
an error naming the field — v4 will not populate it, and says so rather than
leaving it silently at its zero value.

The time zone that was the second argument to `Map` is now a client-level setting
(`WithLocation`, default UTC), stamped onto every record. Override per call with
`rec.TimeIn(field, loc)`. If your v3 code passed anything other than `time.UTC`
— typically `time.Local` — set that location on the client, or every decoded
date and timestamp shifts by your UTC offset, with no error:

```go
c, err := filemaker.New(host, db, filemaker.BasicAuth(user, pass),
    filemaker.WithLocation(time.Local), // what v3 code passed to Map and Time
)
```

`Decode` also stops touching fields it has no tag for. v3's `Map` filled every
exported field of a supported type, looking up an untagged one as the FileMaker
field `""` and an `fm:"-"` one as the field `"-"`, so it overwrote both with
their zero values. v4 leaves them as they were.

### Errors

v3 reported a host failure as an untyped error with the text
`failed at host: <message> (<code>)`. v4 returns a `*filemaker.APIError`
carrying the host's messages, whose `Code()` is the primary FileMaker error
code. Replace string matching on the error text with `errors.As`, or match the
common codes with `errors.Is` and the sentinels `ErrNoRecords` (401),
`ErrRecordModified` (306), and `ErrInvalidToken` (952):

```go
// v3
if err != nil && strings.Contains(err.Error(), "(952)") { /* reconnect */ }

// v4
var apiErr *filemaker.APIError
switch {
case errors.Is(err, filemaker.ErrInvalidToken):
    // or let WithReauthOnInvalidToken retry for you
case errors.As(err, &apiErr) && apiErr.Code() == 504:
    // a specific FileMaker error code
}
```

A response that never reached the Data API — a proxy answering instead of the
host, or a body that is not a Data API response — is a `*filemaker.HTTPError`
instead. For a write, that means the change may or may not have been applied.

## Behavior changes to watch

- **Login timing.** A bad host/credential failed at `New` in v3; in v4 it
  surfaces on the first operation (or on an explicit `Authenticate(ctx)`).
- **`LastActivity()` is zero on a new client**, and moves on every successful
  request rather than only on `Find`. Idle-reconnect checks built on it fire at
  once; use `WithReauthOnIdle` instead (see *Sessions*).
- **Dates read as UTC by default.** `Time(field)` and `Decode` use the client's
  `WithLocation`, default UTC, where v3 code passed a location on every call.
  Set `WithLocation(time.Local)` if that is what you passed.
- **A Go `bool` is no longer written as `1`/`0`.** Wrap it in
  `filemaker.Bool` (see *Writing values*).
- **Host errors are typed.** Match `*APIError` codes and the sentinels with
  `errors.Is` / `errors.As`, not the error text (see *Errors*).
- **`Decode` is flat**, not recursive (see above).
- **`Decode` reports every field it cannot fill.** v3's `Map` returned nothing
  and left such fields at their zero value. v4 returns one error naming each
  `fm`-tagged field it could not fill, and the record:
  - a tag naming a field the record does not have — usually a typo, or a field
    not on the layout — wraps `ErrMissingField`; tag the field
    `fm:"Name,optional"` if it may be absent;
  - a value that does not convert wraps the accessor's error: `ErrNotNumber`
    (text in a number field), `ErrNotInteger` (`3.7`, `7.0` or `1e3` into an
    `int`), `ErrOutOfRange` (`300` into an `int8`), `ErrUnknownFormat` (a date
    that does not parse);
  - a field whose type is unsupported — an unsigned or slice field, a nested
    struct, or a defined type over a supported one (`type Status string` is not
    `string`) — or an unknown tag option. This depends on the struct alone, so
    it surfaces on the first decode.

  Empty fields still decode to the zero value without error, every field that
  can be filled still is, and a number field decodes into a `string` field as
  its exact text, as `String` reads it. Test for a cause with `errors.Is`. To keep v3's leniency, log
  the error and use the struct. Untagged and `fm:"-"` fields are unaffected.
  Supported types: `string`, `int`, `int8`, `int16`, `int32`, `int64`,
  `float32`, `float64`, `Number`, `bool`, `time.Duration`, `time.Time`,
  `*time.Time`.
- **Raw number values are `filemaker.Number`, not `float64`.** `Get` and
  `Fields` return number fields as `Number`, so a type assertion or switch
  on `float64` — or a comparison such as `rec.Get("Qty") == 7.0` — compiles but
  no longer matches. Read values through the accessors, or assert `Number`. A
  number field holding text a user typed into it is a `string`, as before.
- **`Int`/`Int64` read only integer notation, and no longer truncate or
  overflow.** A value written with a fractional part (`3.7`) returns
  `ErrNotInteger` (and `0`) rather than `3`; a value beyond the type returns `ErrOutOfRange` rather than an undefined
  result. So does a whole number the host sends in another notation — `7.0` or
  `1e3`, which v3 read as 7 and 1000. The host sends the text as it was typed
  into FileMaker, so such entries do occur; read them with `Float64`, or parse
  `rec.Number(field)` yourself. `Decode` reports these values for `int` fields
  as errors too (see above).
- **Integers are written as strings.** `FieldData{"Qty": 7}` is sent as
  `"Qty":"7"`, and `Number` values likewise, because the host rounds a
  JSON-number input to a double before storing it but stores a numeric string
  exactly. The stored value is an ordinary number — it sorts, finds, and
  calculates the same. This shows only in request bodies (`WithDebug` logs, or a
  test server that compares them). Floats are still sent as JSON numbers.
- **`Find` returns `FindResponse`**, not `[]Record`. Reach for `res.Records`.
  Every data operation returns its own `…Response` type the same way, even where
  the host reports nothing (`SetGlobalFieldsResponse`).
- **Removed API.** Replacements for each are above.
  - Session: the `Session` type (now `Client`) and its exported fields,
    `Destroy` (now `Logout`), `NewRecord`, and `ResponseBody`.
  - Finds: `FindCommand`, `FindCriterion`, `NewFindCommand`, `NewFindRequest`,
    `NewFindCriterion`, and their `Limit`, `Offset`, `Sort`, `Omit`,
    `AddRequest`, and `AddCriterion` methods. `FindRequest` survives as a struct.
  - Record writes: `Set`, `Commit`, `Create`, `Delete`, `Reset`,
    `CommitToContainer`, `CommitFileToContainer`, and `GetContainerData`.
  - Record fields: `ID`, `Layout`, `FieldData` (now methods), `StagedChanges`,
    and `Session`.
  - Accessors: every `…E` form, `Int8`/`Int16`/`Int32`/`Float32`, and `Map`
    (now `Decode`).

## New in v4

Beyond the migration, v4 adds capabilities v3 lacked:

- **Reading records:** `Get` / `GetByID` for a single record, and `GetRange`
  for records in insertion order.
- **Duplicating records:** `Duplicate` / `DuplicateByID`.
- **Scripts:** `RunScript`, and `WithScript` (plus `WithPrerequestScript` and
  `WithPresortScript`) on any record operation.
- **Portals:** read related records with `Record.Portal`, write them with
  `WithPortalData`, and delete them with `WithDeleteRelated`.
- **Metadata:** `ProductInfo`, `Databases`, `Layouts`, `LayoutMetadata`, and
  `Scripts`.
- **Global fields:** `SetGlobalFields`.
- **Container downloads by URL:** `DownloadFromContainerByURL`.
- **Optimistic concurrency** (opt-in): `IfUnchanged` / `WithModID`.
- **Write-value wrappers:** `Bool`, `Date`, `Timestamp`, `Time`, and `Duration`.
- **Exact numbers** beyond `float64` precision: `Number`.
- **ISO date writes:** `WithDateFormat(DateFormatISO)`.
- **Token re-authentication** (opt-in): `WithReauthOnIdle` and
  `WithReauthOnInvalidToken`.

See the [README](../README.md) and the
[package reference](https://pkg.go.dev/github.com/MjukBiltvatt/go-filemaker/v4)
for these.
