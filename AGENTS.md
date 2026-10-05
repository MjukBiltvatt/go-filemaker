# Agent guidance

## Deciding which file code goes in

**A file owns a domain: its types, its endpoints, its serializers, and its option vocabulary.** `metadata.go` is the exemplar — it holds the metadata types, the metadata endpoints, and its own `LayoutMetadataOption`. `find.go`, `get.go`, `data.go`, and `script.go` follow the same shape, and `record.go`, `params.go`, `options.go`, `errors.go`, and `values.go` are the concept files for the nouns that cut across endpoints.

Two rules follow from that:

- **A cross-cutting type lives in the file for the concept it names, not with the endpoints that return it.** `ScriptOutcomes` is embedded in every endpoint response and belongs in `script.go`. `SortRule` is the value type for `WithSort`, so it belongs in `options.go` next to `EntryMode` — not in `find.go`, even though Find consumes it. Likewise `PortalData` is the value type for `WithPortalData`, so it sits with it in `options.go`, while `PortalRow`, which `Record.Portal` returns, is in `record.go`.
- **Never add a file named for a processing layer or a category of thing** — no `types.go`, `wire.go`, `serialization.go`, or `helpers.go`. Those scatter each concept across files by stage, so understanding one thing means opening four. `response.go` is not an exception to this: it exists because `responseBody` is a single type with methods and a private helper (`recordWire`), the way `net/http/response.go` does. It is not a home for response types in general.

Go has no enforced layout, and the standard library organizes both by type (`net/http`) and by operation (`encoding/json`) depending on the package's primary axis. This package's axis is the endpoint domain. When a new type has no obvious home, name its concept first; if the concept has no file and would not justify one, leave the type in the domain file that already owns its vocabulary rather than inventing a layer file.

## Endpoint result types

Every data endpoint method returns `<Method>Response`, which corresponds to the endpoint's `response` object — even when that object is empty, so a key the host adds later becomes a new field rather than a new return value. A pair of methods over one endpoint shares a type (`Get`/`GetByID` → `GetResponse`, `UploadToContainer`/`UploadToContainerByID` → `UploadResponse`). A named object inside `response` gets its own type, named for the object (`ProductInfo`, `Database`), held as a field of the `…Response`. Field names are Go-idiomatic, not wire keys: rename or regroup where it serves the caller (`data` → `Records`; the six script keys → `ScriptOutcomes`).

An endpoint with no Data API body (the container stream) still returns `<Method>Response`, corresponding to the HTTP response as a whole. Headers become fields only when they describe the payload (`Content-Type`) and are lifted into typed fields; never expose `http.Header` or transport details such as status codes or caching headers.

`Authenticate` and `Logout` return only `error`: they manage the client's session rather than return data to the caller, and the session token stays private. Local accessors such as `LastActivity` make no request and are not endpoint methods.

## Request shape

Responses regroup for the caller; requests keep the Data API's structure. Each part of a request gets one type or option, named after it, in the host's own terms — its **faithful form**: `FieldData` is `fieldData`, `PortalData` is `portalData`, a `FindRequest` is one `query` entry, `WithEntryMode` is `options.entrymode`. Offsets stay 1-based, host defaults stay the host's, find criteria stay FileMaker find syntax, and `deleteRelated` stays in `FieldData` where the API puts it. The library hides only how a part is sent: wire key spellings (`recordId`/`modId` in a portal row, `limit.<name>` in a find body versus `_limit.<name>` in a query string) and value encodings (exact-digit numbers, date formats).

Convenience is a layer on top of the faithful form, built from a value the caller has read: `Update(rec)` over `UpdateByID`, `IfUnchanged()` over `WithModID(rec.ModID())`, `Asc`/`Desc` over `SortRule`. The faithful form stays usable on its own beside each shortcut. Options are named `With…` or `If…`, and a sub-part of one request — a portal row — is a value passed to its option, the way a `SortRule` is passed to `WithSort`.

## Record identity in errors

An endpoint that addresses a record by layout and ID returns every failure after its argument checks through `recordErr` in `errors.go`, so the error names the record. The helper holds the rule and its reasoning. A new record-addressed endpoint calls `recordErr`; change the rule there, in one place. Reading a value is covered the same way: the field accessors shared by `Record` and `PortalRow` live on the embedded `fields` type in `record.go`, and route their errors through `origin.identify`, which calls `recordErr` (or `portalRowErr` for a portal row). A new accessor goes on `fields` and uses `read`.

## Keeping the public docs in sync

`README.md` and `doc.go` document the exported API at a **coarse grain**. When you add, remove, or rename an exported *operation* (a `*Client` method) or *client option* (`With…` passed to `New`), update the matching table in `README.md` — the **"What it does"** capability map or the **Client options** table — and reconcile `doc.go` if the change touches the overview narrative (lifecycle, reading/writing model, concurrency, errors).

This is intentionally not a row-per-symbol rule: per-field accessors, per-request options, value wrappers, and individual types live in godoc only and never appear in the README, so most changes need no edit here.

## Keeping `docs/integration-testing.md` in sync

`docs/integration-testing.md` is the authoritative guide for running the integration suite against a real FileMaker Server. It must stay in sync with `integration_test.go`. Apply the rules below whenever you add, remove, or rename integration tests or their fixture requirements.

### Coverage table

The **"What the suite covers"** table in section 4 of the doc has one row per test function. Every function matching `TestIntegration*` in `integration_test.go` must have a corresponding row. The row format is:

```
| `TestIntegrationFoo` | Brief description of what it exercises |
```

- **Adding a test** — append a row. Keep the table ordered the same as the functions appear in `integration_test.go`.
- **Removing a test** — remove its row.
- **Renaming a test** — update the row name.

### Fixture requirements

When a test needs something that cannot be created programmatically at runtime (a specific named script, a named layout, a special field, a portal configuration), document it in the relevant setup subsection (§1 Tables, §1 Scripts, §1 Layouts, §1 Special-character layout). The doc is read by humans configuring a brand-new FileMaker file; every manual prerequisite must be described there.

- New script fixtures go under **§1 Scripts**.
- New layout fixtures go under **§1 Layouts**.
- New table fields go in the **`ParentTable`** or **`ChildTable`** field tables in **§1 Tables**.
- New environment variables go in the **§2 Configure `.env`** tables (Required or Optional).

### What does NOT belong in the doc

- Internal test helpers, shared setup functions, or unexported types — these are implementation details.
- Go-level build tag or test flag details beyond what is already there; refer readers to `Makefile` targets.

### Checklist before finishing a change to `integration_test.go`

- [ ] Every `TestIntegration*` function has a row in the coverage table.
- [ ] Table row order matches function order in `integration_test.go`.
- [ ] Any new fixture (script, layout, field, env var) is documented in its setup subsection.
- [ ] Any removed fixture is removed from the doc.
