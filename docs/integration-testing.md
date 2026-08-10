# Integration testing

The integration suite exercises the client against a **real FileMaker Server**,
covering the behavior that the unit tests (which use `httptest` mocks) cannot
vouch for: the host's type coercion, server-side validation, optimistic-lock
conflicts, portal semantics, container I/O, and session re-authentication.

The tests live in [`integration_test.go`](../integration_test.go) behind the
`integration` build tag, so the default `go test ./...` stays hermetic and needs
no server. When the required environment variables are absent, every integration
test **skips** rather than fails, so the suite is safe to run anywhere.

## 1. Prepare the FileMaker file

Create a **dedicated, disposable** database — never point the suite at
production data. The tests create and delete records on every run (they
self-clean via `t.Cleanup`, leaving the layout empty on success).

### Account and server

- Host the file on a server with the **FileMaker Data API enabled**.
- The account's privilege set must grant the **`fmrest`** extended privilege
  ("Access via FileMaker Data API"). Without it every call fails at login,
  which looks like bad credentials.

### Tables

Two tables in a one-to-many relationship.

**`ParentTable`** — the layout's table. Fields:

| Field | Type | Notes |
| --- | --- | --- |
| `TextField` | Text | The lookup/marker key the tests find records by |
| `NumberField` | Number | |
| `TextSecondary` | Text | Left out of an update, to prove a patch doesn't clear it |
| `DateField` | Date | |
| `TimestampField` | Timestamp | |
| `TimeField` | Time | Time-of-day round-trip (clock value and >24h duration) |
| `ContainerField` | Container | Upload/download round-trip |
| `RequiredField` | Text | **Validation:** Not Empty, **"Validate always"** (not "only during data entry"), and **"Allow user to override during data entry" OFF** |
| `SoftRequiredField` | Text | **Validation:** Not Empty, **"Only during data entry"** (not "Validate always"), and **"Allow user to override during data entry" ON**. Used by `TestIntegrationWithEntryModeScript` |
| `AutoEnterField` | Text | **Auto-Enter:** Calculated value `"auto"`. **"Prohibit modification of value during data entry" ON**. Used by `TestIntegrationWithProhibitModeScript` |
| `GlobalField` | Text | **Storage:** Global (Options → Storage → "Use global storage"). Used by `TestIntegrationSetGlobalFields` |
| `Id` | Number _or_ Text | Relationship match key — Number with auto-enter **serial**, or Text with an auto-enter UUID; must be the **same type** as `ChildTable::ParentId`. Never touched by the tests |

**`ChildTable`** — the related table shown in the portal. Fields:

| Field | Type | Notes |
| --- | --- | --- |
| `ChildText` | Text | The field the portal test reads/writes |
| `ParentId` | Same as `ParentTable::Id` | Foreign key, matched to `ParentTable::Id` — Number or Text, but it **must match that field's type**. Auto-populated by the host. Never touched by the tests |

### Relationship

`ParentTable::Id  =  ChildTable::ParentId` (one-to-many). Both of these options
must be enabled on the **`ChildTable`** side of the relationship dialog:

- **Allow creation of records in this table via this relationship** — so adding
  a portal row links it automatically.
- **Delete related records in this table when a record is deleted in the other
  table** — so deleting a parent cascades to its children and keeps teardown
  clean.

### Layouts

The test suite uses two layouts. Their names are **constants in the test code**
and are not configurable via environment variables.

**`ParentTable`** (the `itLayout` constant) — the primary test layout, based on
the `ParentTable` occurrence:

- Place all eleven non-`Id` `ParentTable` fields on it (the Data API only sees
  fields that are on the layout): `TextField`, `NumberField`, `TextSecondary`,
  `DateField`, `TimestampField`, `TimeField`, `ContainerField`, `RequiredField`,
  `SoftRequiredField`, `AutoEnterField`, `GlobalField`.
- Add a **portal** showing `ChildTable`, with `ChildText` in it. Leave the
  portal object name unset (or set it to `ChildTable`) so the Data API keys the
  returned portal data by the table-occurrence name. Set it to show **exactly 3
  rows** ("Number of rows: 3" in the portal setup). `TestIntegrationPortalPaging`
  depends on this: a default find caps returned portal rows at the portal's row
  count, and the test seeds more rows than that to confirm both the cap and that
  an explicit `WithPortalLimit` overrides it. (Keep this in sync with the
  `portalRowHeight` constant in the test.)

**`ParentTableResponse`** (the `itResponseLayout` constant) — a minimal layout
used by `TestIntegrationWithResponseLayout` to confirm that `WithResponseLayout`
causes the host to shape the response using a different field set. Based on the
same `ParentTable` occurrence, but exposing **only `TextField`** — no other
fields, no portal. When a find or get is issued against `ParentTable` with
`WithResponseLayout("ParentTableResponse")`, the host returns field data through
this layout, so `NumberField` and all other fields are absent from the response.

### Scripts

`TestIntegrationScripts` reads the database's script catalog. Scripts are
design-time objects the Data API cannot create, so the suite cannot seed them —
the file must define a small fixture by hand. Open **Scripts → Script Workspace**
and create:

- **At least one script at the top level** (not inside any folder) — proves a
  leaf entry parses.
- **At least one script folder that contains at least one script** — the nested
  script is what exercises the recursive `folderScriptNames` decode; an empty
  folder would not.

For the catalog test above the names and bodies are irrelevant (those scripts are
never run); only the shape matters. A minimal setup is one top-level script plus
one folder with one script inside it. Grant the test account access to the scripts
(or use **Full Access**) so the Data API returns them.

`TestIntegrationScriptResults` additionally runs scripts and checks their
results, so it needs **two specifically named, runnable fixture scripts**:

- **`EchoParam`** (top level), a single step: **`Exit Script [ Get ( ScriptParameter ) ]`**.
  It returns its parameter unchanged, so the test can confirm the script ran and
  the result/error round-trip. (This can double as the required top-level script
  above.)
- **`TriggerError`** (top level), a script that runs and deliberately ends in a
  non-zero error state — for example:

  ```
  Set Error Capture [ On ]
  Perform Find [ ]      # with a stored request that matches no records → error 401
  ```

  Any deterministic error works; the test only checks that the script ran and did
  *not* succeed (`Ran() && !OK()`), not a specific code. It confirms that a script
  which errors leaves the request itself successful while reporting the error in
  the response.

The missing-script half of that test runs a name that does not exist and needs no
fixture. Grant the test account access to both fixtures (or use **Full Access**).

### Layouts

`TestIntegrationLayouts` reads the database's layout catalog. The `ParentTable`
layout you already created above supplies the top-level entry,
and the test confirms it shows up in the catalog. To exercise the recursive
`folderLayoutNames` decode, the file also needs **at least one layout folder that
contains a layout** — in **Manage → Layouts**, create a folder and put any layout
inside it (a duplicate of `ParentTable` is fine; it is never used). As with
scripts, an empty folder will not satisfy the test.

### Special-character layout

`TestIntegrationSpecialLayoutNames` checks that the client percent-escapes
URL-reserved characters in a layout name into the request path. It needs a layout
named exactly **`Sales #1`** — easiest is to **duplicate the `ParentTable` layout
and rename the copy** so it carries the same fields. The test does a small
create → find → delete cycle on it (the `#` and space must escape to `%23`/`%20`,
or the path is corrupted and the host rejects the call).

> **Note — slashes are unsupported.** A layout name containing a literal `/`
> cannot be reached through the Data API: FileMaker's web server returns 404 for
> the `%2F`-encoded slash before the request reaches the Data API, regardless of
> how the client encodes the path. There is no client-side workaround, so the
> test deliberately does not cover it. Avoid `/` in layout names you intend to
> reach via the API.

## 2. Configure `.env`

Copy the template and fill it in. `.env` is gitignored; `.env.example` is
committed.

```sh
cp .env.example .env
```

Required:

| Variable | Value |
| --- | --- |
| `FM_HOST` | Host, with or without scheme (https assumed) |
| `FM_DATABASE` | Database name |
| `FM_USERNAME` | Account name |
| `FM_PASSWORD` | Account password |

Optional:

| Variable | Effect |
| --- | --- |
| `FM_LOCATION` | IANA zone (e.g. `Europe/Stockholm`); enables the `WithLocation` timestamp test. Every other test runs with the default location, UTC |
| `FM_DEBUG` | `1` logs every HTTP request/response to stderr (auth redacted) |
| `FM_ALLOW_INSECURE` | `1` permits a plaintext `http://` host (dev only) |

## 3. Run

```sh
make integration                              # whole suite
make integration RUN=TestIntegrationFindQuery # one test (RUN is a -run regexp)
make integration RUN='CRUD|Portal'            # several
make integration FM_DEBUG=1 RUN=TestIntegrationPortal   # one test, with HTTP logging
```

`make integration` sources `.env`, then runs the `integration`-tagged tests with
`-count=1` (the cache is disabled because results depend on the live host and the
environment, neither of which is part of Go's test-cache key — without it a stale
all-skipped run would be replayed).

The hermetic unit tests stay separate:

```sh
make test    # go test ./...  — no server needed
```

## What the suite covers

| Test | Exercises |
| --- | --- |
| `TestIntegrationProductInfo` | Unauthenticated connectivity / metadata |
| `TestIntegrationDatabases` | Database listing (Basic-auth path) |
| `TestIntegrationScripts` | Script catalog listing; recursive folder hierarchy (needs the script fixture) |
| `TestIntegrationLayouts` | Layout catalog listing; recursive folder hierarchy; `ParentTable` present (needs the layout folder fixture) |
| `TestIntegrationLayoutMetadata` | Single-layout metadata for `ParentTable`: doubles as an environment check — every documented field present with the right result type, only `RequiredField` and `SoftRequiredField` Not-Empty, `ChildTable` portal exposes `ChildText` (no extra fixture) |
| `TestIntegrationCRUD` | Create → find → update (patch) → delete; number coercion |
| `TestIntegrationSpecialLayoutNames` | URL-reserved characters in a layout name are escaped into the path (needs the `Sales #1` layout) |
| `TestIntegrationDateTime` | Date/timestamp wrappers and read-back parsing |
| `TestIntegrationDateTimeLocation` | `WithLocation` zone applied on read (needs `FM_LOCATION`) |
| `TestIntegrationRequiredField` | Server-side Not-Empty validation (code 509) |
| `TestIntegrationFindNoMatch` | Empty result is a nil error, not `ErrNoRecords` |
| `TestIntegrationContainer` | Container upload + download round-trip |
| `TestIntegrationPortal` | Related-record add / edit / delete via portals |
| `TestIntegrationPortalPaging` | Portal row cap: default find returns at most the portal's configured row count; `WithPortalLimit` overrides it (needs the portal configured to 3 rows) |
| `TestIntegrationUpdateWithModID` | Optimistic lock by mod ID; conflict → 306 |
| `TestIntegrationUpdateIfUnchanged` | Record-relative optimistic lock; conflict → 306 |
| `TestIntegrationScriptResults` | `WithScript` on Update and Find: echo-param round-trip, script error without request failure, missing script → `*APIError` code 104 (needs the `EchoParam` and `TriggerError` fixtures) |
| `TestIntegrationFindQuery` | Sort, limit, offset, omit, OR across requests, `DataInfo` |
| `TestIntegrationFindMultiSort` | Multiple sort fields applied in order; result sequence is deterministic |
| `TestIntegrationReauthOnInvalidToken` | Expired-session recovery via `WithReauthOnInvalidToken` |
| `TestIntegrationTimeOfDay` | Time field round-trip: `Time`/`Duration` wrappers and `Time()`/`Duration()` getters |
| `TestIntegrationGet` | `GetByID` and `Get` fetch a single record; field data round-trips correctly |
| `TestIntegrationGetRange` | `GetRange` with `WithLimit`/`WithSort`; returned record count and `DataInfo` are correct |
| `TestIntegrationSetGlobalFields` | `SetGlobalFields` sets a global field value; host accepts the write (needs the `GlobalField` fixture) |
| `TestIntegrationWithDateFormatISO` | `WithDateFormat(DateFormatISO)` writes ISO + sends `dateformats=2` |
| `TestIntegrationRunScript` | `RunScript` dedicated endpoint: echo param round-trip, script error without request failure, missing script → `*APIError` code 104 (needs the `EchoParam` and `TriggerError` fixtures) |
| `TestIntegrationDuplicate` | `DuplicateByID` duplicates a record; the copy receives a new record ID and carries the original's field values |
| `TestIntegrationWithEntryModeScript` | `WithEntryMode(EntryModeScript)` bypasses "Only during data entry" validation — host rejects the write without it (code 509), accepts with it |
| `TestIntegrationWithProhibitModeScript` | `WithProhibitMode(EntryModeScript)` bypasses a "Prohibit modification" field — host rejects the write without it (code 201), accepts with it and stores the supplied value |
| `TestIntegrationWithResponseLayout` | `WithResponseLayout` causes the host to shape the response through a different layout — `NumberField` is absent when the response layout exposes only `TextField` (needs the `ParentTableResponse` layout) |

## Date formats (`WithDateFormat`)

A few facts established against a live host, in case the behavior ever looks
surprising:

- The Data API's `dateformats` parameter controls only the **textual
  representation** of date/timestamp values, never the stored value or the
  instant. The stored value is format-independent.
- It is **representation-only on reads** (`0` US, `1` locale, `2` ISO; default
  `0`) and **input-parsing on writes**. The library uses it on writes only:
  `WithDateFormat` is opt-in, sends the parameter only when set, and requires
  FileMaker Server 2023+. Reads stay tolerant and parse either format.
- A write quirk: ISO timestamps must use a **space** separator
  (`2006-01-02 15:04:05`); a `T` is rejected on input (error 500) — even though
  the host *emits* a `T` when returning `dateformats=2` reads.
- The write format only affects how a record displays "as entered" / in data
  entry in FileMaker clients (durably, per record); it does not change the
  value. Apply explicit date formatting on the layout for consistent browse
  display — though the entered format is still used during subsequent value entry.

## Common first-run snags

In rough order of how often they bite:

1. **`fmrest` extended privilege not granted** — fails at login, looks like bad
   credentials.
2. **`RequiredField` set to "only during data entry"** — the Data API bypasses
   that, so the validation test fails its "create should have been rejected"
   path instead of seeing code 509.
3. **`SoftRequiredField` set to "Validate always"** — it then behaves like
   `RequiredField` and cannot be bypassed by `entrymode=script`, so
   `TestIntegrationWithEntryModeScript` fails its "create with EntryModeScript
   should have succeeded" path.
4. **`AutoEnterField` missing "Prohibit modification of value during data entry"** —
   the host accepts a write that supplies a value even without `prohibitmode=script`,
   so `TestIntegrationWithProhibitModeScript` fails its "create without
   ProhibitModeScript should have been rejected" path instead of seeing code 201.
5. **A field exists in the table but isn't on the `ParentTable` layout** — the
   Data API won't see it, so it won't round-trip.
6. **The portal object has a custom name** — portal data comes back under that
   name instead of `ChildTable`, so the portal test can't find its rows.
7. **Host configured with a non-US date format** — surfaced by
   `TestIntegrationDateTime` (by design; it confirms the wrappers' format against
   the live host).
8. **No script fixture (or an empty folder)** — `TestIntegrationScripts` fails
   asking for a top-level script and a folder that *contains* a script; an empty
   folder doesn't exercise the nested decode and won't satisfy it.
9. **No layout folder fixture (or an empty one)** — `TestIntegrationLayouts`
   fails asking for a folder that *contains* a layout, for the same reason.
