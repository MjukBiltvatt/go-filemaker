// Package filemaker is a goroutine-safe Go client for the FileMaker Data API.
//
// A single [Client] talks to one database on one FileMaker Server host. It is
// safe to share across goroutines: concurrent finds, creates, and updates run
// without data races, and session tokens are managed internally. Records the
// client returns are immutable, caller-owned values.
//
// # Client lifecycle
//
// [New] performs no network I/O — its only error is cheap argument validation.
// The session is established lazily on the first operation that needs it, or
// eagerly with [Client.Authenticate]. [Client.Logout] ends the session, but the
// client stays usable: a later operation re-authenticates. Token refresh is
// opt-in via [WithReauthOnInvalidToken] (reactive, on a 952) and
// [WithReauthOnIdle] (proactive, before an idle request).
//
//	c, err := filemaker.New("https://fms.example.com", "MyDatabase", "user", "pass")
//	if err != nil {
//	    return err
//	}
//	defer c.Logout(ctx)
//
// # Reading
//
// [Client.Find] runs one or more declarative [FindRequest] values: criteria are
// AND-ed within a request and OR-ed across requests. [Client.Get] fetches one
// record by ID, and [Client.GetRange] pages through a layout. Each returns
// [Record] values whose data is read through typed accessors — [Record.String],
// [Record.Int], [Record.Time], [Record.Decode], and so on.
//
//	res, err := c.Find(ctx, "People",
//	    []filemaker.FindRequest{{Criteria: filemaker.Criteria{"Lastname": "==Johnson"}}},
//	    filemaker.WithSort(filemaker.Asc("Firstname")),
//	    filemaker.WithLimit(10),
//	)
//	for _, rec := range res.Records {
//	    fmt.Println(rec.ID(), rec.String("Firstname"), rec.Int("Age"))
//	}
//
// # Writing (data-in / data-out)
//
// There are no staged changes on a record. Callers build a [FieldData] map and
// pass it to [Client.Create] or [Client.Update]; the call returns the host's
// acknowledgement (a record ID, a mod ID), not a refreshed record. To read a
// record back after a write, issue a [Client.Find] or [Client.Get].
//
// FieldData is marshaled faithfully — string and number values are sent as-is.
// The optional wrappers [Bool], [Date], [Timestamp], [Time], and [Duration]
// render Go values in the formats FileMaker expects and slot directly into the
// map.
//
//	created, err := c.Create(ctx, "People", filemaker.FieldData{
//	    "Firstname": "Mark",
//	    "Active":    filemaker.Bool(true),
//	    "DOB":       filemaker.Date(birthday),
//	})
//
// # Bare versus ByID
//
// Write and container operations come in pairs. The bare verb takes a [Record]
// you already hold — typically from a find — and reads its layout and ID:
// [Client.Update], [Client.Delete], [Client.Duplicate],
// [Client.UploadToContainer], [Client.DownloadFromContainer]. The ByID/ByURL
// form takes raw addressing for when you hold only an identifier (e.g. a record
// ID from [CreateResponse]): [Client.UpdateByID], [Client.DeleteByID], and so on.
//
// # Concurrency
//
// A *[Client] is safe for concurrent use by multiple goroutines. A [Record] it
// returns is owned by the caller; concurrent reads are safe, and because records
// are immutable there is nothing to synchronize.
//
// Session teardown is the one place ordering matters. [Client.Logout] invalidates
// the token other goroutines are still using, so operations racing it fail and —
// with [WithReauthOnInvalidToken] — open a new session that outlives the logout.
// Let in-flight work finish before logging out.
//
// # Errors
//
// A failed request returns an *[APIError] carrying the host's status messages;
// inspect it with errors.As and branch on [APIError.Code]. A few host codes that
// carry control-flow meaning have errors.Is-friendly sentinels: [ErrRecordModified]
// (306), [ErrNoRecords] (401), and [ErrInvalidToken] (952). Find treats "no
// records" (401) as an empty result with a nil error, not a failure.
package filemaker
