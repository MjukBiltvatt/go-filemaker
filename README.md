# go-filemaker

A goroutine-safe Go client for the [FileMaker Data API](https://help.claris.com/en/data-api-guide/).

- **Client-object design** — records are plain, immutable data; all operations are methods on a `*Client`.
- **Goroutine-safe** — share one client across goroutines; session tokens are managed internally.
- **Declarative finds** — write queries as plain struct literals, no method chaining.

> **v4 is a breaking redesign.** Migrating from v3? See the [migration guide](docs/migration-v3-to-v4.md).

## Install

```sh
go get github.com/MjukBiltvatt/go-filemaker/v4
```

```go
import "github.com/MjukBiltvatt/go-filemaker/v4"
```

The import path ends in `/v4`; the package name is `filemaker`.

## Quickstart

```go
package main

import (
	"context"
	"fmt"
	"log"

	"github.com/MjukBiltvatt/go-filemaker/v4"
)

func main() {
	ctx := context.Background()

	// New does no network I/O — the session is established on first use.
	c, err := filemaker.New("https://fms.example.com", "MyDatabase", "user", "pass")
	if err != nil {
		log.Fatal(err)
	}
	defer c.Logout(ctx)

	// Find: criteria are AND-ed within a request, OR-ed across requests.
	res, err := c.Find(ctx, "People",
		[]filemaker.FindRequest{
			{Criteria: filemaker.Criteria{"Lastname": "==Johnson", "Age": ">18"}},
		},
		filemaker.WithSort(filemaker.SortRule{Field: "Lastname", Order: filemaker.SortAscending}),
		filemaker.WithLimit(10),
	)
	if err != nil {
		log.Fatal(err)
	}
	for _, rec := range res.Records {
		fmt.Println(rec.ID(), rec.String("Firstname"), rec.Int("Age"))
	}

	// Create returns the host's acknowledgement, not the new record.
	created, err := c.Create(ctx, "People", filemaker.FieldData{
		"Firstname": "Mark",
		"Lastname":  "Johnson",
	})
	if err != nil {
		log.Fatal(err)
	}

	// You hold an ID, not a record → use the ByID forms.
	if _, err := c.UpdateByID(ctx, "People", created.RecordID, filemaker.FieldData{"Age": 42}); err != nil {
		log.Fatal(err)
	}
	if _, err := c.DeleteByID(ctx, "People", created.RecordID); err != nil {
		log.Fatal(err)
	}
}
```

## What it does

Every operation is a method on `*Client`. Full reference on [pkg.go.dev](https://pkg.go.dev/github.com/MjukBiltvatt/go-filemaker/v4).

| Area | Entry points |
| --- | --- |
| **Read** | `Find`, `Get` / `GetByID`, `GetRange` |
| **Write** | `Create`, `Update` / `UpdateByID`, `Delete` / `DeleteByID`, `Duplicate` / `DuplicateByID` |
| **Containers** | `UploadToContainer` / `UploadToContainerByID`, `DownloadFromContainer` / `DownloadFromContainerByURL` |
| **Scripts** | `RunScript`; `WithScript` / `WithPrerequestScript` / `WithPresortScript` on any operation |
| **Globals** | `SetGlobalFields` |
| **Metadata** | `Databases`, `ProductInfo`, `Scripts`, `Layouts`, `LayoutMetadata` |
| **Session** | `Authenticate`, `Logout`, `LastActivity` |

### Bare vs. `ByID`

Write and container operations come in pairs:

- **Bare** verbs take a `Record` you already hold (e.g. from a `Find`) and read its layout and ID — `Update(ctx, rec, …)`, `Delete(ctx, rec)`, `DownloadFromContainer(ctx, rec, field)`.
- **`ByID`/`ByURL`** forms take raw addressing for when you hold only an identifier (e.g. `created.RecordID`) — `UpdateByID(ctx, layout, id, …)`, `DownloadFromContainerByURL(ctx, url)`.

```go
// Working from a find, you hold records → bare forms:
for _, rec := range res.Records {
	_, _ = c.Update(ctx, rec, filemaker.FieldData{"Active": filemaker.Bool(false)})
}
```

## Finding records

A `Find` takes a slice of `FindRequest`. Within one request the `Criteria` are AND-ed; separate requests are OR-ed. Set `Omit` to exclude matches.

```go
res, err := c.Find(ctx, "People", []filemaker.FindRequest{
	{Criteria: filemaker.Criteria{"City": "Stockholm", "Age": "18...30"}},
	{Criteria: filemaker.Criteria{"Lastname": "==Johnson"}, Omit: true},
})
```

Criteria values are **FileMaker find expressions**, not plain literals — operators are characters inside the value: `==Mark` (exact), `Mark*` (wildcard), `>10`, `1...10` (range), `=` (empty), `*` (not empty). Characters like `@ * = < >` are interpreted, not matched, so values built from arbitrary input must be escaped with a backslash.

Shape the result with `WithSort`, `WithLimit`, `WithOffset`, and the portal options `WithPortals` / `WithPortalLimit` / `WithPortalOffset`. The response carries the matched `Records` plus a `DataInfo` with the host's counts (`FoundCount`, `TotalRecordCount`, …) for pagination.

## Reading field values

Records are immutable values read through typed accessors. Each numeric/time accessor has a paired `…E` form returning an error instead of the zero value.

```go
rec.ID()                 // host record ID
rec.ModID()              // modification ID (optimistic concurrency)
rec.String("Firstname")
rec.Int("Age")           // also Int64, Float64
rec.Bool("Active")
rec.Time("Created")      // uses the client's WithLocation; TimeIn overrides
rec.Has("Notes")         // distinguishes absent from present-but-empty
rec.Fields()             // copy of all field values (FieldData)
rec.Portals()            // deep copy of portal rows (PortalData)
```

`Decode` maps a record's `fm`-tagged fields onto a struct (it is **not** recursive — decode nested structs explicitly):

```go
var p struct {
	First string `fm:"Firstname"`
	Age   int    `fm:"Age"`
}
if err := rec.Decode(&p); err != nil { /* … */ }
```

## Writing field values

Build a `FieldData` map. String and number values are sent as-is; the optional wrappers render Go types in FileMaker's expected formats:

```go
c.Create(ctx, "People", filemaker.FieldData{
	"Name":    "Mark",                          // raw
	"Active":  filemaker.Bool(true),            // -> 1 / 0
	"DOB":     filemaker.Date(birthday),        // -> "06/23/1990"
	"Created": filemaker.Timestamp(time.Now()), // -> "06/23/1990 15:04:05"
	"Alarm":   filemaker.Time(alarmTime),       // -> "15:04:05"
	"Worked":  filemaker.Duration(elapsed),     // -> "37:30:00"
})
```

Dates render in US format by default; build the client with `WithDateFormat(filemaker.DateFormatISO)` to write and interpret ISO 8601 instead.

**Optimistic concurrency** is opt-in on `Update`: `IfUnchanged()` locks against the record's own mod ID, `WithModID(id)` against a specific one. A stale write returns `ErrRecordModified`.

## Client options

Pass to `New`:

| Option | Effect                                                                                              |
| --- |-----------------------------------------------------------------------------------------------------|
| `WithTimeout(d)` | Per-request HTTP timeout (default 30 seconds).                                                      |
| `WithLocation(loc)` | Time zone stamped onto returned records (default UTC).                                              |
| `WithDateFormat(f)` | `DateFormatUS` (default) or `DateFormatISO` for date/timestamp I/O.                                 |
| `WithReauthOnInvalidToken()` | Re-authenticate and retry once on an expired token (952).                                           |
| `WithReauthOnIdle(d…)` | Refresh proactively before a request idle past the timeout (default `DefaultIdleTimeout`, ~14 min). |
| `WithInsecureHTTP()` | Permit a plain `http://` host (development only).                                                   |
| `WithDebug(w)` | Dump requests/responses to `w` with the `Authorization` header redacted.                            |

## Errors

A failed request returns an `*APIError` carrying the host's status messages:

```go
res, err := c.Find(ctx, "People", reqs)
if err != nil {
	var apiErr *filemaker.APIError
	if errors.As(err, &apiErr) {
		log.Printf("FileMaker error %d: %s", apiErr.Code(), apiErr)
	}
}
```

A few codes that carry control-flow meaning have `errors.Is`-friendly sentinels — `ErrRecordModified` (306), `ErrNoRecords` (401), `ErrInvalidToken` (952). `Find` treats "no records" as an empty result with a nil error, not a failure.

## Concurrency

A `*Client` is safe for concurrent use by multiple goroutines. A `Record` it returns is caller-owned; because records are immutable, concurrent reads need no synchronization.

## License

[MIT](LICENSE) © Mjuk Biltvätt Sverige AB
