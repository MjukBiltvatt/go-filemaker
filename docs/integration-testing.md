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
| `ContainerField` | Container | Upload/download round-trip |
| `RequiredField` | Text | **Validation:** Not Empty, **"Validate always"** (not "only during data entry"), and **"Allow user to override during data entry" OFF** |
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

### Layout

A layout named **`ParentTable`**, based on the `ParentTable` occurrence:

- Place the seven `ParentTable` fields on it (the Data API only sees fields that
  are on the layout).
- Add a **portal** showing `ChildTable`, with `ChildText` in it. Leave the
  portal object name unset (or set it to `ChildTable`) so the Data API keys the
  returned portal data by the table-occurrence name.

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
| `FM_LAYOUT` | Name of the layout to test against — the one built on `ParentTable` (named `ParentTable` here) |

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
| `TestIntegrationCRUD` | Create → find → update (patch) → delete; number coercion |
| `TestIntegrationDateTime` | Date/timestamp wrappers and read-back parsing |
| `TestIntegrationDateTimeLocation` | `WithLocation` zone applied on read (needs `FM_LOCATION`) |
| `TestIntegrationRequiredField` | Server-side Not-Empty validation (code 509) |
| `TestIntegrationFindNoMatch` | Empty result is a nil error, not `ErrNoRecords` |
| `TestIntegrationContainer` | Container upload + download round-trip |
| `TestIntegrationPortal` | Related-record add / edit / delete via portals |
| `TestIntegrationUpdateWithModID` | Optimistic lock by mod ID; conflict → 306 |
| `TestIntegrationUpdateIfUnchanged` | Record-relative optimistic lock; conflict → 306 |
| `TestIntegrationFindQuery` | Sort, limit, offset, omit, OR across requests, `DataInfo` |
| `TestIntegrationReauthOnInvalidToken` | Expired-session recovery via `WithReauthOnInvalidToken` |

## Common first-run snags

In rough order of how often they bite:

1. **`fmrest` extended privilege not granted** — fails at login, looks like bad
   credentials.
2. **`RequiredField` set to "only during data entry"** — the Data API bypasses
   that, so the validation test fails its "create should have been rejected"
   path instead of seeing code 509.
3. **A field exists in the table but isn't on the `ParentTable` layout** — the
   Data API won't see it, so it won't round-trip.
4. **The portal object has a custom name** — portal data comes back under that
   name instead of `ChildTable`, so the portal test can't find its rows.
5. **Host configured with a non-US date format** — surfaced by
   `TestIntegrationDateTime` (by design; it confirms the wrappers' format against
   the live host).
