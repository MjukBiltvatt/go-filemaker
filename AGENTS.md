# Agent guidance

## Deciding which file code goes in

**A file owns a domain: its types, its endpoints, its serializers, and its option vocabulary.** `metadata.go` is the exemplar — it holds the metadata types, the metadata endpoints, and its own `LayoutMetadataOption`. `find.go`, `get.go`, `data.go`, and `script.go` follow the same shape, and `record.go`, `params.go`, `options.go`, `errors.go`, and `values.go` are the concept files for the nouns that cut across endpoints.

Two rules follow from that:

- **A cross-cutting type lives in the file for the concept it names, not with the endpoints that return it.** `ScriptOutcomes` is embedded in every endpoint response and belongs in `script.go`. `SortRule` is the value type for `WithSort`, so it belongs in `options.go` next to `EntryMode` — not in `find.go`, even though Find consumes it. `PortalData` is in `record.go` because `Record.Portals` returns it.
- **Never add a file named for a processing layer or a category of thing** — no `types.go`, `wire.go`, `serialization.go`, or `helpers.go`. Those scatter each concept across files by stage, so understanding one thing means opening four. `response.go` is not an exception to this: it exists because `responseBody` is a single type with methods and a private helper (`recordWire`), the way `net/http/response.go` does. It is not a home for response types in general.

Go has no enforced layout, and the standard library organizes both by type (`net/http`) and by operation (`encoding/json`) depending on the package's primary axis. This package's axis is the endpoint domain. When a new type has no obvious home, name its concept first; if the concept has no file and would not justify one, leave the type in the domain file that already owns its vocabulary rather than inventing a layer file.

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
