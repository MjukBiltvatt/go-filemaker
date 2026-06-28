//go:build integration

// Package-level integration tests that exercise the client against a real
// FileMaker Server. They are excluded from the default `go test ./...` run by
// the `integration` build tag, so the hermetic unit tests (httptest mocks) stay
// the fast default and need no server.
//
// # Running
//
// Configure the target via environment variables, then run with the tag:
//
//	set -a; source .env; set +a
//	go test -tags=integration -run Integration -v ./...
//
// or simply `make integration` (see the Makefile target).
//
// Required variables:
//
//	FM_HOST       host, with or without scheme (e.g. https://fms.example.com)
//	FM_DATABASE   database name
//	FM_USERNAME   account name
//	FM_PASSWORD   account password
//	FM_LAYOUT     a dedicated, disposable test layout (see schema below)
//
// Optional:
//
//	FM_LOCATION         IANA zone (e.g. Europe/Stockholm); when set, the
//	                    WithLocation timestamp round-trip test runs. Every other
//	                    test runs with the default location, UTC.
//	FM_DEBUG            "1" to log every HTTP request and response to stderr
//	                    (auth redacted) via WithDebug — useful for diagnosing a
//	                    failing test against the live host.
//	FM_ALLOW_INSECURE   "1" to permit a plaintext http:// host (dev only).
//
// When the required variables are unset, every test in this file skips rather
// than fails, so the suite is safe to run anywhere.
//
// # Expected test layout
//
// Point FM_LAYOUT at a throwaway layout — never production data — on a table
// named ParentTable, exposing at least these fields, all editable via the Data
// API:
//
//	TextField       text; also the unique lookup key the tests find records by.
//	NumberField     number.
//	TextSecondary   text; a field left out of an update, to prove a patch does
//	                not clear the fields it omits.
//	DateField       date.
//	TimestampField  timestamp.
//	TimeField       time (time-of-day).
//	ContainerField  container.
//	RequiredField   text, validated Not Empty, "Validate always" (not just during
//	                data entry) and not user-overridable, so the host enforces it
//	                on the Data API. Every write test populates it; the validation
//	                test omits it on purpose to assert the host rejects the record.
//
// The portal test additionally needs a second table, ChildTable, in a
// one-to-many relationship with the layout's table (ParentTable):
//
//   - ChildTable has a text field ChildText, exposed on the layout through a
//     table occurrence also named ChildTable (portal data is keyed by that name).
//   - The relationship ParentTable -> ChildTable matches on a key (ParentTable::Id
//     to ChildTable::ParentId; the tests never touch it, the host auto-populates
//     it). On the ChildTable side of the relationship, enable both "Allow
//     creation of records in this table via this relationship" (so adding a
//     portal row links it automatically) and "Delete related records in this
//     table when a record is deleted in the other table" (so deleting a parent
//     cascades, keeping teardown clean).
//   - A portal on FM_LAYOUT showing ChildTable with ChildText. Leave the portal
//     object name unset (or equal to ChildTable) so the Data API keys the
//     returned portal data by the table-occurrence name.
//
// Every test that writes data deletes it again via t.Cleanup, so a passing run
// leaves the layout empty.
//
// # Expected scripts and layout folder
//
// TestIntegrationScripts and TestIntegrationLayouts need small design-time
// fixtures the Data API cannot create:
//
//   - Scripts: at least one runnable script at the top level and at least one
//     script folder that contains a script (the nested script is what exercises
//     the recursive folderScriptNames decode).
//   - Layouts: at least one layout folder that contains a layout (exercising the
//     recursive folderLayoutNames decode). The configured FM_LAYOUT supplies the
//     top-level layout, and the test also confirms it appears in the catalog.
//
// The names do not matter; only the shape does.
package filemaker

import (
	"bytes"
	"context"
	"errors"
	"os"
	"slices"
	"sort"
	"testing"
	"time"
)

const (
	fieldText          = "TextField"
	fieldNumber        = "NumberField"
	fieldTextSecondary = "TextSecondary"
	fieldDate          = "DateField"
	fieldTimestamp     = "TimestampField"
	fieldTime          = "TimeField"
	fieldRequired      = "RequiredField"
	fieldContainer     = "ContainerField"

	// Portal (related-record) addressing. portalName is the ChildTable table
	// occurrence the layout's portal shows; the host keys portal data by it.
	// Within a row, field values are fully qualified as "TableOccurrence::Field",
	// but the row's own record ID is a plain "recordId" key — both when read back
	// from a find response and when written to identify an existing row in an
	// edit. (Namespacing it, "ChildTable::recordId", makes the host read it as a
	// field and reject the edit with code 102 "Field is missing".)
	portalName       = "ChildTable"
	fieldChildText   = portalName + "::ChildText"
	keyChildRecordID = "recordId"

	// scriptEcho is a fixture script the database must define (see
	// docs/integration-testing.md): Exit Script [Get(ScriptParameter)], so it
	// returns its parameter unchanged. TestIntegrationScriptResults uses it to
	// confirm a script ran and its result/error round-trip.
	scriptEcho = "EchoParam"

	// scriptFail is a fixture script that runs and deliberately ends in a
	// non-zero error state (see docs/integration-testing.md). It checks the
	// "ran and errored" outcome — distinct from a missing script (a request
	// error) and from no script at all.
	scriptFail = "TriggerError"
)

// itClient is the single shared session for the whole integration suite, built
// once in TestMain. FileMaker licenses cap concurrent Data API sessions, so
// tests reuse this rather than authenticating individually. It is nil when the
// suite is unconfigured, which makes requireServer skip.
var (
	itClient *Client
	itLayout string

	// Credentials captured in TestMain so sibling clients with extra options
	// (e.g. WithLocation) can be built mid-test via buildITClient.
	itHost, itDatabase, itUsername, itPassword string
	itAllowInsecure, itDebug                   bool
)

func TestMain(m *testing.M) {
	itHost = os.Getenv("FM_HOST")
	itDatabase = os.Getenv("FM_DATABASE")
	itUsername = os.Getenv("FM_USERNAME")
	itPassword = os.Getenv("FM_PASSWORD")
	itLayout = os.Getenv("FM_LAYOUT")
	itAllowInsecure = os.Getenv("FM_ALLOW_INSECURE") == "1"
	itDebug = os.Getenv("FM_DEBUG") == "1"

	// Unconfigured: run anyway so the tests report as skipped (via
	// requireServer) instead of failing. itClient stays nil.
	if itHost == "" || itDatabase == "" || itUsername == "" || itPassword == "" {
		os.Exit(m.Run())
	}

	c, err := New(itHost, itDatabase, itUsername, itPassword, itClientOptions()...)
	if err != nil {
		// A construction error is a configuration mistake, not a test failure;
		// fail loudly so it is not mistaken for "no server".
		panic("filemaker integration: New failed: " + err.Error())
	}
	if err := c.Authenticate(context.Background()); err != nil {
		panic("filemaker integration: authentication failed: " + err.Error())
	}
	itClient = c

	code := m.Run()

	// Free the session slot on the host rather than leaving it to time out.
	_ = c.Logout(context.Background())
	os.Exit(code)
}

// itBaseOptions returns the options shared by every integration client except
// the reactive-reauth default — so a test can build a client deliberately
// without it — plus any extra ones the caller appends.
func itBaseOptions(extra ...Option) []Option {
	var opts []Option
	if itAllowInsecure {
		opts = append(opts, WithInsecureHTTP())
	}
	if itDebug {
		// Logs every request and response to stderr (auth headers redacted by
		// the transport). Bodies are printed in full, so it is opt-in.
		opts = append(opts, WithDebug(os.Stderr))
	}
	return append(opts, extra...)
}

// itClientOptions is itBaseOptions plus WithReauthOnInvalidToken — the default
// for the shared client and for buildITClient siblings.
func itClientOptions(extra ...Option) []Option {
	return itBaseOptions(append([]Option{WithReauthOnInvalidToken()}, extra...)...)
}

// buildITClient authenticates a fresh sibling session with the suite's
// credentials plus extra options, and registers its logout. Use it when a test
// needs a client configured differently from the shared itClient (e.g. a
// specific WithLocation). It consumes another concurrent session for the
// duration of the test, so prefer the shared client where the difference does
// not matter.
func buildITClient(t *testing.T, extra ...Option) *Client {
	t.Helper()
	c, err := New(itHost, itDatabase, itUsername, itPassword, itClientOptions(extra...)...)
	if err != nil {
		t.Fatalf("New (sibling client): %v", err)
	}
	if err := c.Authenticate(context.Background()); err != nil {
		t.Fatalf("Authenticate (sibling client): %v", err)
	}
	t.Cleanup(func() { _ = c.Logout(context.Background()) })
	return c
}

// requireServer skips the calling test unless the suite is configured against a
// real server.
func requireServer(t *testing.T) {
	t.Helper()
	if itClient == nil {
		t.Skip("integration server not configured; set FM_HOST, FM_DATABASE, FM_USERNAME, FM_PASSWORD, FM_LAYOUT")
	}
}

// requireLayout skips the calling test unless a test layout is configured.
func requireLayout(t *testing.T) {
	t.Helper()
	requireServer(t)
	if itLayout == "" {
		t.Skip("FM_LAYOUT not set; skipping layout-scoped test")
	}
}

// TestIntegrationProductInfo is a cheap, unauthenticated connectivity check: it
// confirms the host is reachable and speaking the Data API.
func TestIntegrationProductInfo(t *testing.T) {
	requireServer(t)

	info, err := itClient.ProductInfo(context.Background())
	if err != nil {
		t.Fatalf("ProductInfo: %v", err)
	}
	if info.Name == "" {
		t.Errorf("ProductInfo returned empty Name: %+v", info)
	}
	t.Logf("host product: %+v", info)
}

// TestIntegrationDatabases lists the Data API-enabled databases. Depending on
// the host's "Filter Databases" setting this is authenticated or not; either
// way the configured database should appear.
func TestIntegrationDatabases(t *testing.T) {
	requireServer(t)

	dbs, err := itClient.Databases(context.Background())
	if err != nil {
		t.Fatalf("Databases: %v", err)
	}
	t.Logf("databases: %+v", dbs)
}

// TestIntegrationScripts lists the scripts defined in the configured database
// and checks that the folder hierarchy round-trips. Scripts are design-time
// objects the Data API cannot create, so the test relies on a small fixture the
// database must define (see docs/integration-testing.md): at least one runnable
// script at the top level and at least one folder that itself contains a script.
// The folder-with-a-script requirement is what actually exercises the recursive
// folderScriptNames decode — an empty folder would parse without ever reaching a
// nested entry.
func TestIntegrationScripts(t *testing.T) {
	requireServer(t)

	scripts, err := itClient.Scripts(context.Background())
	if err != nil {
		t.Fatalf("Scripts: %v", err)
	}
	t.Logf("scripts: %+v", scripts)

	var topLevelScript, folderWithScript bool
	for _, s := range scripts {
		if !s.IsFolder {
			topLevelScript = true
			continue
		}
		for _, nested := range s.FolderScriptNames {
			if !nested.IsFolder {
				folderWithScript = true
			}
		}
	}
	if !topLevelScript {
		t.Error("no top-level script found; the test database must define at least one script outside any folder (see docs/integration-testing.md)")
	}
	if !folderWithScript {
		t.Error("no folder containing a script found; the test database must define a script folder with at least one script in it (see docs/integration-testing.md)")
	}
}

// TestIntegrationLayouts lists the database's layouts and checks the folder
// hierarchy round-trips. Like scripts, layouts are design-time objects, so the
// test relies on a fixture (see docs/integration-testing.md): at least one
// folder that itself contains a layout, which exercises the recursive
// folderLayoutNames decode. It also confirms the configured FM_LAYOUT appears
// somewhere in the catalog — a real-data check the scripts test cannot make.
func TestIntegrationLayouts(t *testing.T) {
	requireServer(t)

	layouts, err := itClient.Layouts(context.Background())
	if err != nil {
		t.Fatalf("Layouts: %v", err)
	}
	t.Logf("layouts: %+v", layouts)

	if !findLayout(layouts, itLayout) {
		t.Errorf("configured layout %q not found in the catalog", itLayout)
	}

	var folderWithLayout bool
	for _, l := range layouts {
		if !l.IsFolder {
			continue
		}
		for _, nested := range l.FolderLayoutNames {
			if !nested.IsFolder {
				folderWithLayout = true
			}
		}
	}
	if !folderWithLayout {
		t.Error("no folder containing a layout found; the test database must define a layout folder with at least one layout in it (see docs/integration-testing.md)")
	}
}

// TestIntegrationLayoutMetadata fetches the metadata for FM_LAYOUT and verifies
// it against the schema documented in docs/integration-testing.md. Because the
// endpoint reports the whole layout in one call, this doubles as a check that
// the test environment is set up correctly: every documented field must be
// present with the expected result type, only RequiredField may be Not-Empty,
// and the ChildTable portal must expose ChildText. Each failure points at a
// specific misconfiguration (and the doc) rather than a library bug.
//
// It verifies the *declared* schema, not validation enforcement: the metadata
// notEmpty flag shows a Not-Empty validation exists but cannot reveal whether it
// is set to "Validate always", so TestIntegrationRequiredField remains the real
// check that the host rejects an empty write. No extra fixture is needed — the
// existing test layout supplies everything.
func TestIntegrationLayoutMetadata(t *testing.T) {
	requireLayout(t)

	meta, err := itClient.LayoutMetadata(context.Background(), itLayout)
	if err != nil {
		t.Fatalf("LayoutMetadata: %v", err)
	}
	t.Logf("layout metadata: %+v", meta)

	fields := make(map[string]FieldMetadata, len(meta.FieldMetadata))
	for _, f := range meta.FieldMetadata {
		fields[f.Name] = f
	}

	// Every documented ParentTable field must be on the layout with the result
	// type its row in the doc's schema table implies. A missing field or wrong
	// type is an environment problem, not a library bug — hence the doc pointer.
	// (Result strings, e.g. "timeStamp" casing, are the host's; adjust here if a
	// future server version reports them differently.)
	wantResult := map[string]string{
		fieldText:          "text",
		fieldNumber:        "number",
		fieldTextSecondary: "text",
		fieldDate:          "date",
		fieldTimestamp:     "timeStamp",
		fieldTime:          "time",
		fieldContainer:     "container",
		fieldRequired:      "text",
	}
	for name, result := range wantResult {
		f, ok := fields[name]
		if !ok {
			t.Errorf("field %q not on layout %q — place it on the layout (see docs/integration-testing.md)", name, itLayout)
			continue
		}
		if f.Result != result {
			t.Errorf("field %q result = %q, want %q — check the field's type (see docs/integration-testing.md)", name, f.Result, result)
		}
	}

	// RequiredField is the only field defined Not Empty; the write tests rely on
	// every other field being freely omittable (e.g. TextSecondary is left out of
	// an update). An unexpected Not-Empty field would break those tests in a more
	// confusing place, so assert the validation is declared on RequiredField and
	// nowhere else.
	if rf, ok := fields[fieldRequired]; ok && !rf.NotEmpty {
		t.Errorf("%s NotEmpty = false, want true — set it Not Empty, \"Validate always\" (see docs/integration-testing.md)", fieldRequired)
	}
	for name, f := range fields {
		if name != fieldRequired && f.NotEmpty {
			t.Errorf("field %q is Not-Empty but only %s should be — the write tests omit other fields (see docs/integration-testing.md)", name, fieldRequired)
		}
	}

	// Note: the host reports TimeOfDay=false for a plain Time field, so that flag
	// is not a reliable "this is a time-of-day field" signal and is not asserted —
	// TimeField's behavior is covered by TestIntegrationTimeOfDay instead.

	// The ChildTable portal is keyed by its table-occurrence name and exposes
	// ChildText (fully qualified, as on the layout) as a text field.
	portal, ok := meta.PortalMetadata[portalName]
	if !ok {
		t.Fatalf("portal %q not in portalMetaData (keys: %v) — add a ChildTable portal to the layout (see docs/integration-testing.md)", portalName, portalKeys(meta.PortalMetadata))
	}
	var childText *FieldMetadata
	for i := range portal {
		if portal[i].Name == fieldChildText {
			childText = &portal[i]
		}
	}
	if childText == nil {
		t.Errorf("portal %q does not expose %q — add ChildText to the portal (see docs/integration-testing.md)", portalName, fieldChildText)
	} else if childText.Result != "text" {
		t.Errorf("portal field %q result = %q, want \"text\"", fieldChildText, childText.Result)
	}
}

// portalKeys returns the portal names present in a portalMetaData map, for
// diagnostics.
func portalKeys(m map[string][]FieldMetadata) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

// findLayout reports whether a layout with the given name exists anywhere in the
// catalog, descending into folders.
func findLayout(layouts []Layout, name string) bool {
	for _, l := range layouts {
		if !l.IsFolder && l.Name == name {
			return true
		}
		if l.IsFolder && findLayout(l.FolderLayoutNames, name) {
			return true
		}
	}
	return false
}

// TestIntegrationCRUD exercises the full record lifecycle against a real host:
// create, find back, assert the host's type coercion (number -> float64),
// update a patch, re-find, then delete. It self-cleans via t.Cleanup so a failed
// assertion mid-test still removes the record.
func TestIntegrationCRUD(t *testing.T) {
	requireLayout(t)
	ctx := context.Background()

	// A unique marker so the find matches exactly this record even if the
	// layout is not perfectly empty.
	marker := "go-filemaker-it-" + time.Now().UTC().Format("20060102T150405.000000000")
	const secondaryText = "untouched by the patch"

	created, err := itClient.Create(ctx, itLayout, FieldData{
		fieldText:          marker,
		fieldNumber:        7,
		fieldTextSecondary: secondaryText,
		fieldRequired:      "present",
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if created.RecordID == "" {
		t.Fatal("Create returned empty RecordID")
	}
	t.Cleanup(func() {
		if _, err := itClient.DeleteByID(context.Background(), itLayout, created.RecordID); err != nil {
			t.Errorf("cleanup DeleteByID(%s): %v", created.RecordID, err)
		}
	})

	// Find it back by the unique marker.
	found, err := itClient.Find(ctx, itLayout, []FindRequest{{Criteria: map[string]string{fieldText: "==" + marker}}})
	if err != nil {
		t.Fatalf("Find: %v", err)
	}
	if len(found.Records) != 1 {
		t.Fatalf("Find returned %d records, want 1", len(found.Records))
	}
	rec := found.Records[0]
	if got := rec.String(fieldText); got != marker {
		t.Errorf("%s = %q, want %q", fieldText, got, marker)
	}
	// FileMaker number fields decode as float64; Int() bridges that.
	if got := rec.Int(fieldNumber); got != 7 {
		t.Errorf("%s = %d, want 7", fieldNumber, got)
	}

	// Patch only the number field; every other field must be left untouched.
	if _, err := itClient.Update(ctx, rec, FieldData{fieldNumber: 42}); err != nil {
		t.Fatalf("Update: %v", err)
	}

	reFound, err := itClient.Find(ctx, itLayout, []FindRequest{{Criteria: map[string]string{fieldText: "==" + marker}}})
	if err != nil {
		t.Fatalf("Find after update: %v", err)
	}
	if len(reFound.Records) != 1 {
		t.Fatalf("Find after update returned %d records, want 1", len(reFound.Records))
	}
	updated := reFound.Records[0]
	if got := updated.Int(fieldNumber); got != 42 {
		t.Errorf("%s after update = %d, want 42", fieldNumber, got)
	}
	// A field absent from the patch must survive it unchanged.
	if got := updated.String(fieldTextSecondary); got != secondaryText {
		t.Errorf("%s after update = %q, want %q (patch should not clear fields it omits)", fieldTextSecondary, got, secondaryText)
	}
}

// TestIntegrationSpecialLayoutNames verifies that a layout name containing
// URL-reserved characters round-trips through the request path. It runs a
// create → find → delete cycle (exercising recordsURL, findURL, and recordURL)
// against "Sales #1", a duplicate of ParentTable that must exist in the test
// database (see docs/integration-testing.md). The space and '#' escape to %20 and
// %23; without escaping the '#' truncates the path into a dropped fragment and
// the host rejects the request (error 1704).
//
// A literal '/' in a layout name is a known, unfixable exception: FileMaker's web
// server returns 404 for the %2F-encoded slash before the request reaches the
// Data API, so such layouts are unreachable no matter how the client encodes the
// path. It is deliberately not exercised here.
func TestIntegrationSpecialLayoutNames(t *testing.T) {
	requireServer(t)
	ctx := context.Background()

	const layout = "Sales #1"
	marker := "go-filemaker-it-" + time.Now().UTC().Format("20060102T150405.000000000")

	created, err := itClient.Create(ctx, layout, FieldData{
		fieldText:     marker,
		fieldRequired: "present",
	})
	if err != nil {
		t.Fatalf("Create on %q: %v", layout, err)
	}
	t.Cleanup(func() {
		if _, err := itClient.DeleteByID(context.Background(), layout, created.RecordID); err != nil {
			t.Errorf("cleanup DeleteByID(%q, %s): %v", layout, created.RecordID, err)
		}
	})

	found, err := itClient.Find(ctx, layout, []FindRequest{{Criteria: map[string]string{fieldText: "==" + marker}}})
	if err != nil {
		t.Fatalf("Find on %q: %v", layout, err)
	}
	if len(found.Records) != 1 {
		t.Fatalf("Find on %q returned %d records, want 1", layout, len(found.Records))
	}
}

// wallClock formats a time as its zone-free wall-clock representation, so two
// times can be compared on calendar/clock components alone — independent of the
// host's and client's time zones, which a date/timestamp round-trip should not
// shift.
func wallClock(t time.Time) string {
	return t.Format("2006-01-02 15:04:05")
}

// TestIntegrationDateTime round-trips a date and a timestamp through the host.
// This is the coverage the mock-based unit tests cannot provide: it confirms the
// host accepts the US-format the Date/Timestamp wrappers emit (values.go) and
// that the Time accessors parse the host's stored representation back to the same
// wall-clock value. If the host is configured non-US, a failure here is a real
// library/host incompatibility, not a flaky test.
func TestIntegrationDateTime(t *testing.T) {
	requireLayout(t)
	ctx := context.Background()

	// Fixed, zone-free values; compared on wall-clock components below.
	date := time.Date(1990, 6, 23, 0, 0, 0, 0, time.UTC)
	ts := time.Date(2026, 6, 26, 15, 4, 5, 0, time.UTC)
	marker := "go-filemaker-it-dt-" + time.Now().UTC().Format("20060102T150405.000000000")

	created, err := itClient.Create(ctx, itLayout, FieldData{
		fieldText:      marker,
		fieldDate:      Date(date),
		fieldTimestamp: Timestamp(ts),
		fieldRequired:  "present",
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	t.Cleanup(func() {
		if _, err := itClient.DeleteByID(context.Background(), itLayout, created.RecordID); err != nil {
			t.Errorf("cleanup DeleteByID(%s): %v", created.RecordID, err)
		}
	})

	found, err := itClient.Find(ctx, itLayout, []FindRequest{{Criteria: map[string]string{fieldText: "==" + marker}}})
	if err != nil {
		t.Fatalf("Find: %v", err)
	}
	if len(found.Records) != 1 {
		t.Fatalf("Find returned %d records, want 1", len(found.Records))
	}
	rec := found.Records[0]

	gotDate, err := rec.TimeE(fieldDate)
	if err != nil {
		t.Errorf("TimeE(%s) = %v; stored value %q did not parse", fieldDate, err, rec.String(fieldDate))
	} else if got, want := wallClock(gotDate), wallClock(date); got != want {
		t.Errorf("%s round-trip = %s, want %s", fieldDate, got, want)
	}

	gotTS, err := rec.TimeE(fieldTimestamp)
	if err != nil {
		t.Errorf("TimeE(%s) = %v; stored value %q did not parse", fieldTimestamp, err, rec.String(fieldTimestamp))
	} else if got, want := wallClock(gotTS), wallClock(ts); got != want {
		t.Errorf("%s round-trip = %s, want %s", fieldTimestamp, got, want)
	}
}

// TestIntegrationDateTimeLocation verifies the WithLocation option end to end:
// a record read through a client configured for FM_LOCATION carries that zone on
// its timestamp accessors, while the wall-clock value is unchanged. FileMaker
// stores timestamps zone-free, so the location is applied on read-back, not on
// the stored value. Runs only when FM_LOCATION names a valid IANA zone.
func TestIntegrationDateTimeLocation(t *testing.T) {
	requireLayout(t)
	name := os.Getenv("FM_LOCATION")
	if name == "" {
		t.Skip("FM_LOCATION not set; skipping location round-trip")
	}
	loc, err := time.LoadLocation(name)
	if err != nil {
		t.Fatalf("FM_LOCATION %q: %v", name, err)
	}
	ctx := context.Background()

	// Sibling client that interprets date/timestamp fields in loc.
	c := buildITClient(t, WithLocation(loc))

	ts := time.Date(2026, 6, 26, 15, 4, 5, 0, time.UTC)
	marker := "go-filemaker-it-loc-" + time.Now().UTC().Format("20060102T150405.000000000")

	created, err := c.Create(ctx, itLayout, FieldData{
		fieldText:      marker,
		fieldTimestamp: Timestamp(ts),
		fieldRequired:  "present",
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	t.Cleanup(func() {
		if _, err := c.DeleteByID(context.Background(), itLayout, created.RecordID); err != nil {
			t.Errorf("cleanup DeleteByID(%s): %v", created.RecordID, err)
		}
	})

	found, err := c.Find(ctx, itLayout, []FindRequest{{Criteria: map[string]string{fieldText: "==" + marker}}})
	if err != nil {
		t.Fatalf("Find: %v", err)
	}
	if len(found.Records) != 1 {
		t.Fatalf("Find returned %d records, want 1", len(found.Records))
	}

	got, err := found.Records[0].TimeE(fieldTimestamp)
	if err != nil {
		t.Fatalf("TimeE(%s): %v", fieldTimestamp, err)
	}
	// The configured zone is attached on read-back...
	if got.Location().String() != loc.String() {
		t.Errorf("%s location = %s, want %s", fieldTimestamp, got.Location(), loc)
	}
	// ...while the wall-clock value is the same as what was written.
	if w := wallClock(got); w != wallClock(ts) {
		t.Errorf("%s wall clock = %s, want %s", fieldTimestamp, w, wallClock(ts))
	}
}

// TestIntegrationRequiredField confirms the host enforces field validation that
// no mock can reproduce: creating a record that omits a Not-Empty field is
// rejected with FileMaker code 509 ("Field requires a value"), surfaced as an
// *APIError. The library exposes no sentinel for 509, so this also exercises the
// documented "unpack APIError and switch on Code()" pattern.
func TestIntegrationRequiredField(t *testing.T) {
	requireLayout(t)
	ctx := context.Background()

	// Deliberately omit fieldRequired. The host must refuse to store the record.
	created, err := itClient.Create(ctx, itLayout, FieldData{
		fieldText: "go-filemaker-it-missing-required",
	})
	if err == nil {
		// It was stored, which means the layout is misconfigured. Remove the
		// stray record so the run stays clean, then fail with a pointed hint.
		_, _ = itClient.DeleteByID(context.Background(), itLayout, created.RecordID)
		t.Fatalf("Create with %s omitted succeeded; expected validation failure "+
			"(is %s set Not Empty, \"Validate always\", and not user-overridable?)", fieldRequired, fieldRequired)
	}

	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("Create error = %v (%T); want *APIError", err, err)
	}
	const codeFieldRequired = 509 // FileMaker "Field requires a value"
	if apiErr.Code() != codeFieldRequired {
		t.Errorf("validation error code = %d (%v); want %d", apiErr.Code(), apiErr, codeFieldRequired)
	}
}

// TestIntegrationFindNoMatch confirms a query that matches nothing comes back as
// an empty (non-nil) result with a nil error, not an ErrNoRecords surfaced to
// the caller.
func TestIntegrationFindNoMatch(t *testing.T) {
	requireLayout(t)

	res, err := itClient.Find(context.Background(), itLayout, []FindRequest{{Criteria: map[string]string{
		fieldText: "==go-filemaker-it-no-such-record-zzz",
	}}})
	if err != nil {
		t.Fatalf("Find: %v", err)
	}
	if res.Records == nil {
		t.Error("Find returned nil Records slice, want empty non-nil")
	}
	if len(res.Records) != 0 {
		t.Errorf("Find returned %d records, want 0", len(res.Records))
	}
}

// TestIntegrationContainer round-trips bytes through the layout's container
// field: upload, find the record back, download, and compare.
func TestIntegrationContainer(t *testing.T) {
	requireLayout(t)
	ctx := context.Background()

	created, err := itClient.Create(ctx, itLayout, FieldData{
		fieldText:     "go-filemaker-it-container",
		fieldRequired: "present",
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	t.Cleanup(func() {
		if _, err := itClient.DeleteByID(context.Background(), itLayout, created.RecordID); err != nil {
			t.Errorf("cleanup DeleteByID(%s): %v", created.RecordID, err)
		}
	})

	payload := []byte("hello from go-filemaker integration test")
	if err := itClient.UploadToContainerByID(ctx, itLayout, created.RecordID, fieldContainer, "hello.txt", bytes.NewReader(payload)); err != nil {
		t.Fatalf("UploadToContainerByID: %v", err)
	}

	found, err := itClient.Find(ctx, itLayout, []FindRequest{{Criteria: map[string]string{fieldText: "==go-filemaker-it-container"}}})
	if err != nil {
		t.Fatalf("Find: %v", err)
	}
	if len(found.Records) == 0 {
		t.Fatal("Find returned no records after container upload")
	}

	data, err := itClient.DownloadFromContainer(ctx, found.Records[0], fieldContainer)
	if err != nil {
		t.Fatalf("DownloadFromContainer: %v", err)
	}
	if !bytes.Equal(data, payload) {
		t.Errorf("downloaded %d bytes, want %d (content mismatch)", len(data), len(payload))
	}
}

// findParent finds the single record carrying marker in TextField, failing if
// the host returns anything other than exactly one.
func findParent(t *testing.T, ctx context.Context, marker string) Record {
	t.Helper()
	found, err := itClient.Find(ctx, itLayout, []FindRequest{{Criteria: map[string]string{fieldText: "==" + marker}}})
	if err != nil {
		t.Fatalf("Find: %v", err)
	}
	if len(found.Records) != 1 {
		t.Fatalf("Find(%s) returned %d records, want 1", marker, len(found.Records))
	}
	return found.Records[0]
}

// portalRows maps each ChildTable row's ChildText to its record ID, which the
// find response carries as the row's plain "recordId" key.
func portalRows(t *testing.T, rec Record) map[string]string {
	t.Helper()
	rows := rec.Portals()[portalName]
	out := make(map[string]string, len(rows))
	for _, row := range rows {
		text, _ := row[fieldChildText].(string)
		id, _ := row[keyChildRecordID].(string)
		out[text] = id
	}
	return out
}

// TestIntegrationPortal exercises the related-record lifecycle the mocks cannot:
// add rows at Create time through WithPortalData, read them back via
// Record.Portals, edit an existing row by its record ID, and delete one through
// the deleteRelated field-data directive. The edit and delete still go through
// Update, since they address related records that already exist.
func TestIntegrationPortal(t *testing.T) {
	requireLayout(t)
	ctx := context.Background()

	marker := "go-filemaker-it-portal-" + time.Now().UTC().Format("20060102T150405.000000000")
	// Create the parent with two related rows in the same request; a row without
	// a record ID is created.
	created, err := itClient.Create(ctx, itLayout, FieldData{
		fieldText:     marker,
		fieldRequired: "present",
	}, WithPortalData(PortalData{portalName: {
		{fieldChildText: "row-1"},
		{fieldChildText: "row-2"},
	}}))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	t.Cleanup(func() {
		// The relationship deletes related records, so removing the parent
		// cascades to its child rows — no separate child cleanup needed.
		if _, err := itClient.DeleteByID(context.Background(), itLayout, created.RecordID); err != nil {
			t.Errorf("cleanup DeleteByID(%s): %v", created.RecordID, err)
		}
	})

	rows := portalRows(t, findParent(t, ctx, marker))
	if len(rows) != 2 {
		t.Fatalf("after add: %d portal rows, want 2 (%v)", len(rows), rows)
	}
	id1 := rows["row-1"]
	if id1 == "" {
		t.Fatalf("after add: row-1 missing or has no record ID (%v)", rows)
	}
	if _, ok := rows["row-2"]; !ok {
		t.Fatalf("after add: row-2 missing (%v)", rows)
	}

	// Edit row-1 by its record ID. A portal-row edit identifies the existing
	// related record with a plain "recordId" key (see keyChildRecordID).
	editRow := WithPortalData(PortalData{portalName: {
		{keyChildRecordID: id1, fieldChildText: "row-1-edited"},
	}})
	if _, err := itClient.UpdateByID(ctx, itLayout, created.RecordID, nil, editRow); err != nil {
		t.Fatalf("Update (edit row): %v", err)
	}

	rows = portalRows(t, findParent(t, ctx, marker))
	if _, ok := rows["row-1-edited"]; !ok {
		t.Errorf("after edit: row-1-edited missing (%v)", rows)
	}
	if _, ok := rows["row-1"]; ok {
		t.Errorf("after edit: original row-1 still present (%v)", rows)
	}

	// Delete row-2 via the deleteRelated directive ("TO.recordId" in fieldData).
	id2 := rows["row-2"]
	if id2 == "" {
		t.Fatalf("after edit: row-2 has no record ID; cannot delete it (%v)", rows)
	}
	if _, err := itClient.UpdateByID(ctx, itLayout, created.RecordID, FieldData{
		"deleteRelated": portalName + "." + id2,
	}); err != nil {
		t.Fatalf("Update (deleteRelated): %v", err)
	}

	rows = portalRows(t, findParent(t, ctx, marker))
	if len(rows) != 1 {
		t.Fatalf("after delete: %d portal rows, want 1 (%v)", len(rows), rows)
	}
	if _, ok := rows["row-1-edited"]; !ok {
		t.Errorf("after delete: expected row-1-edited to remain (%v)", rows)
	}
}

// TestIntegrationUpdateWithModID exercises the WithModID optimistic-lock option
// against a real host: a conditional update whose mod ID matches applies and
// advances the host's mod ID, and re-using the now-stale mod ID is rejected with
// code 306 (ErrRecordModified) without applying the write.
func TestIntegrationUpdateWithModID(t *testing.T) {
	requireLayout(t)
	ctx := context.Background()

	marker := "go-filemaker-it-modid-" + time.Now().UTC().Format("20060102T150405.000000000")
	created, err := itClient.Create(ctx, itLayout, FieldData{
		fieldText:     marker,
		fieldNumber:   1,
		fieldRequired: "present",
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	t.Cleanup(func() {
		if _, err := itClient.DeleteByID(context.Background(), itLayout, created.RecordID); err != nil {
			t.Errorf("cleanup DeleteByID(%s): %v", created.RecordID, err)
		}
	})

	m0 := findParent(t, ctx, marker).ModID()
	if m0 == "" {
		t.Fatal("found record has no ModID")
	}

	// Happy: the mod ID matches, so the conditional write applies and the host
	// returns a fresh mod ID.
	res, err := itClient.UpdateByID(ctx, itLayout, created.RecordID, FieldData{fieldNumber: 2}, WithModID(m0))
	if err != nil {
		t.Fatalf("UpdateByID WithModID(current): %v", err)
	}
	if res.ModID == "" || res.ModID == m0 {
		t.Errorf("ModID after update = %q, want a new non-empty value (was %s)", res.ModID, m0)
	}
	if got := findParent(t, ctx, marker).Int(fieldNumber); got != 2 {
		t.Errorf("%s = %d after update, want 2", fieldNumber, got)
	}

	// Conflict: m0 is now stale (the host moved on), so the same conditional
	// write is rejected with code 306 and must not apply.
	_, err = itClient.UpdateByID(ctx, itLayout, created.RecordID, FieldData{fieldNumber: 3}, WithModID(m0))
	if !errors.Is(err, ErrRecordModified) {
		t.Errorf("UpdateByID WithModID(stale) error = %v, want ErrRecordModified", err)
	}
	if got := findParent(t, ctx, marker).Int(fieldNumber); got != 2 {
		t.Errorf("%s = %d after rejected update, want 2 (write should not apply)", fieldNumber, got)
	}
}

// TestIntegrationUpdateIfUnchanged exercises the IfUnchanged option, the
// record-relative form of optimistic locking. A first update through a freshly
// read record applies and advances the host's mod ID; re-using the same record
// value — an immutable snapshot still carrying the pre-update mod ID — locks
// against a stale version and is rejected (306). The conflict case doubles as
// proof that a Record's ModID does not move when the host's does.
func TestIntegrationUpdateIfUnchanged(t *testing.T) {
	requireLayout(t)
	ctx := context.Background()

	marker := "go-filemaker-it-ifunchanged-" + time.Now().UTC().Format("20060102T150405.000000000")
	created, err := itClient.Create(ctx, itLayout, FieldData{
		fieldText:     marker,
		fieldNumber:   1,
		fieldRequired: "present",
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	t.Cleanup(func() {
		if _, err := itClient.DeleteByID(context.Background(), itLayout, created.RecordID); err != nil {
			t.Errorf("cleanup DeleteByID(%s): %v", created.RecordID, err)
		}
	})

	recA := findParent(t, ctx, marker)
	if recA.ModID() == "" {
		t.Fatal("found record has no ModID")
	}

	// Happy: recA is current, so the optimistic lock passes, the write applies,
	// and the host's mod ID advances past the one recA holds.
	res, err := itClient.Update(ctx, recA, FieldData{fieldNumber: 2}, IfUnchanged())
	if err != nil {
		t.Fatalf("Update IfUnchanged (current): %v", err)
	}
	if res.ModID == "" || res.ModID == recA.ModID() {
		t.Errorf("ModID after update = %q, want a new non-empty value (was %s)", res.ModID, recA.ModID())
	}
	if got := findParent(t, ctx, marker).Int(fieldNumber); got != 2 {
		t.Errorf("%s = %d after update, want 2", fieldNumber, got)
	}

	// Conflict: recA is an immutable snapshot still holding the pre-update mod
	// ID, so re-using it locks against a stale version and is rejected (306).
	_, err = itClient.Update(ctx, recA, FieldData{fieldNumber: 3}, IfUnchanged())
	if !errors.Is(err, ErrRecordModified) {
		t.Errorf("Update IfUnchanged (stale) error = %v, want ErrRecordModified", err)
	}
	if got := findParent(t, ctx, marker).Int(fieldNumber); got != 2 {
		t.Errorf("%s = %d after rejected update, want 2 (write should not apply)", fieldNumber, got)
	}
}

// TestIntegrationScriptResults exercises the WithScript option and the
// ScriptOutcomes it returns against a real host — coverage the mocks cannot
// provide, since only the host actually runs the script. It relies on the
// scriptEcho fixture (Exit Script [Get(ScriptParameter)]; see
// docs/integration-testing.md): the after-action script's result must echo the
// parameter; a script that runs but ends in an error must leave the request
// successful while ScriptOutcome reports Ran() && !OK() (the scriptFail fixture);
// and naming a script that does not exist must surface as an *APIError with
// FileMaker code 104 ("script is missing").
func TestIntegrationScriptResults(t *testing.T) {
	requireLayout(t)
	ctx := context.Background()

	marker := "go-filemaker-it-script-" + time.Now().UTC().Format("20060102T150405.000000000")
	created, err := itClient.Create(ctx, itLayout, FieldData{
		fieldText:     marker,
		fieldNumber:   1,
		fieldRequired: "present",
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	t.Cleanup(func() {
		if _, err := itClient.DeleteByID(context.Background(), itLayout, created.RecordID); err != nil {
			t.Errorf("cleanup DeleteByID(%s): %v", created.RecordID, err)
		}
	})

	t.Run("Echo", func(t *testing.T) {
		const param = "echo-me-123"
		res, err := itClient.Update(ctx, findParent(t, ctx, marker), FieldData{fieldNumber: 2}, WithScript(scriptEcho, param))
		if err != nil {
			t.Fatalf("Update WithScript(%s): %v", scriptEcho, err)
		}
		if !res.Scripts.Script.OK() {
			t.Errorf("script error = %q, want \"0\" (is %s defined and accessible? see docs/integration-testing.md)", res.Scripts.Script.Error, scriptEcho)
		}
		if res.Scripts.Script.Result != param {
			t.Errorf("script result = %q, want %q (does %s do Exit Script [Get(ScriptParameter)]?)", res.Scripts.Script.Result, param, scriptEcho)
		}
	})

	t.Run("MissingScript", func(t *testing.T) {
		// Naming a script that does not exist is a request-level failure: the host
		// reports FileMaker error 104 ("script is missing") as the primary message,
		// which the client surfaces as an *APIError (not a per-phase scriptError on
		// an otherwise successful response). No fixture needed, so this runs even if
		// scriptEcho is absent.
		_, err := itClient.Update(ctx, findParent(t, ctx, marker), FieldData{fieldNumber: 3}, WithScript("go-filemaker-no-such-script", ""))
		var apiErr *APIError
		if !errors.As(err, &apiErr) {
			t.Fatalf("Update with a missing script: error = %v (%T), want *APIError", err, err)
		}
		const codeScriptMissing = 104
		if apiErr.Code() != codeScriptMissing {
			t.Errorf("error code = %d (%v), want %d (script is missing)", apiErr.Code(), apiErr, codeScriptMissing)
		}
	})

	t.Run("RanWithError", func(t *testing.T) {
		// A script that exists, runs, and ends in an error is the case where a
		// successful response still carries a non-zero per-phase error: the
		// after-action script runs once the edit has committed, so the request
		// succeeds (err == nil) while ScriptOutcome reports Ran() && !OK(). This is
		// the distinction Ran/OK exist for. Relies on the scriptFail fixture; see
		// docs/integration-testing.md. The specific error code is not asserted —
		// only that the script ran and did not succeed.
		res, err := itClient.Update(ctx, findParent(t, ctx, marker), FieldData{fieldNumber: 4}, WithScript(scriptFail, ""))
		if err != nil {
			t.Fatalf("Update WithScript(%s) should still succeed (after-script runs post-commit): %v", scriptFail, err)
		}
		if !res.Scripts.Script.Ran() {
			t.Errorf("script Ran() = false, want true (is %s defined and accessible? see docs/integration-testing.md)", scriptFail)
		}
		if res.Scripts.Script.OK() {
			t.Errorf("script OK() = true, want false; Error = %q (does %s end in an error?)", res.Scripts.Script.Error, scriptFail)
		}
	})
}

// recNumbers returns the NumberField of each record, in order.
func recNumbers(recs []Record) []int {
	out := make([]int, len(recs))
	for i, r := range recs {
		out[i] = r.Int(fieldNumber)
	}
	return out
}

// TestIntegrationFindQuery exercises the query features the host executes and
// the mocks cannot vouch for: sort, limit, offset, omit, OR across requests, and
// the DataInfo counts. It seeds four records sharing a run-unique marker in
// TextField (NumberField 1..4), queries within that set, and cleans them up.
func TestIntegrationFindQuery(t *testing.T) {
	requireLayout(t)
	ctx := context.Background()

	marker := "go-filemaker-it-query-" + time.Now().UTC().Format("20060102T150405.000000000")
	const n = 4
	ids := make([]string, 0, n)
	for i := 1; i <= n; i++ {
		created, err := itClient.Create(ctx, itLayout, FieldData{
			fieldText:     marker,
			fieldNumber:   i,
			fieldRequired: "present",
		})
		if err != nil {
			t.Fatalf("Create %d: %v", i, err)
		}
		ids = append(ids, created.RecordID)
	}
	t.Cleanup(func() {
		for _, id := range ids {
			if _, err := itClient.DeleteByID(context.Background(), itLayout, id); err != nil {
				t.Errorf("cleanup DeleteByID(%s): %v", id, err)
			}
		}
	})

	// mine matches exactly the seeded set.
	mine := FindRequest{Criteria: map[string]string{fieldText: "==" + marker}}

	t.Run("SortDescending", func(t *testing.T) {
		res, err := itClient.Find(ctx, itLayout, []FindRequest{mine},
			WithSort(SortRule{Field: fieldNumber, Order: SortDescending}),
		)
		if err != nil {
			t.Fatalf("Find: %v", err)
		}
		if got := recNumbers(res.Records); !slices.Equal(got, []int{4, 3, 2, 1}) {
			t.Errorf("descending order = %v, want [4 3 2 1]", got)
		}
	})

	t.Run("SortAscendingLimit", func(t *testing.T) {
		res, err := itClient.Find(ctx, itLayout, []FindRequest{mine},
			WithSort(SortRule{Field: fieldNumber, Order: SortAscending}),
			WithLimit(2),
		)
		if err != nil {
			t.Fatalf("Find: %v", err)
		}
		if got := recNumbers(res.Records); !slices.Equal(got, []int{1, 2}) {
			t.Errorf("ascending limit 2 = %v, want [1 2]", got)
		}
		// Limit caps the returned rows but not the found count.
		if res.DataInfo.FoundCount != n {
			t.Errorf("FoundCount = %d, want %d", res.DataInfo.FoundCount, n)
		}
		if res.DataInfo.ReturnedCount != 2 {
			t.Errorf("ReturnedCount = %d, want 2", res.DataInfo.ReturnedCount)
		}
	})

	t.Run("Offset", func(t *testing.T) {
		// Offset is 1-based, so 3 starts at the third record; with limit 2 that
		// is the back half of the ascending set.
		res, err := itClient.Find(ctx, itLayout, []FindRequest{mine},
			WithSort(SortRule{Field: fieldNumber, Order: SortAscending}),
			WithLimit(2),
			WithOffset(3),
		)
		if err != nil {
			t.Fatalf("Find: %v", err)
		}
		if got := recNumbers(res.Records); !slices.Equal(got, []int{3, 4}) {
			t.Errorf("ascending limit 2 offset 3 = %v, want [3 4]", got)
		}
	})

	t.Run("Omit", func(t *testing.T) {
		// Include the whole set, then omit NumberField 1.
		res, err := itClient.Find(ctx, itLayout, []FindRequest{
			mine,
			{Criteria: map[string]string{fieldText: "==" + marker, fieldNumber: "1"}, Omit: true},
		})
		if err != nil {
			t.Fatalf("Find: %v", err)
		}
		got := recNumbers(res.Records)
		sort.Ints(got)
		if !slices.Equal(got, []int{2, 3, 4}) {
			t.Errorf("omit NumberField 1 = %v, want [2 3 4]", got)
		}
		if res.DataInfo.FoundCount != 3 {
			t.Errorf("FoundCount = %d, want 3", res.DataInfo.FoundCount)
		}
	})

	t.Run("MultipleRequestsOR", func(t *testing.T) {
		// Separate requests are alternatives: NumberField 1 OR 4.
		res, err := itClient.Find(ctx, itLayout, []FindRequest{
			{Criteria: map[string]string{fieldText: "==" + marker, fieldNumber: "1"}},
			{Criteria: map[string]string{fieldText: "==" + marker, fieldNumber: "4"}},
		})
		if err != nil {
			t.Fatalf("Find: %v", err)
		}
		got := recNumbers(res.Records)
		sort.Ints(got)
		if !slices.Equal(got, []int{1, 4}) {
			t.Errorf("NumberField 1 OR 4 = %v, want [1 4]", got)
		}
	})

	t.Run("DataInfo", func(t *testing.T) {
		res, err := itClient.Find(ctx, itLayout, []FindRequest{mine})
		if err != nil {
			t.Fatalf("Find: %v", err)
		}
		if res.DataInfo.FoundCount != n || res.DataInfo.ReturnedCount != n {
			t.Errorf("DataInfo = {Found:%d Returned:%d}, want both %d",
				res.DataInfo.FoundCount, res.DataInfo.ReturnedCount, n)
		}
		if res.DataInfo.Layout != itLayout {
			t.Errorf("DataInfo.Layout = %q, want %q", res.DataInfo.Layout, itLayout)
		}
	})
}

// invalidateSession ends a client's session token on the host without disturbing
// the client, so it unknowingly holds a dead token — a faithful stand-in for an
// expired session. In-package access to the unexported token is what makes this
// possible. Returns the now-dead token.
func invalidateSession(t *testing.T, c *Client) string {
	t.Helper()
	c.mu.RLock()
	token := c.token
	c.mu.RUnlock()
	if token == "" {
		t.Fatal("client has no session to invalidate")
	}
	// A throwaway client carries the borrowed token to Logout, which issues
	// DELETE /sessions/{token}. New performs no network I/O, so it opens no
	// session of its own; the token is set directly (single-goroutine test).
	killer, err := New(itHost, itDatabase, itUsername, itPassword, itBaseOptions()...)
	if err != nil {
		t.Fatalf("New (killer): %v", err)
	}
	killer.token = token
	if err := killer.Logout(context.Background()); err != nil {
		t.Fatalf("out-of-band Logout: %v", err)
	}
	return token
}

// TestIntegrationReauthOnInvalidToken proves the reactive re-authentication
// option against a real expired session. The session is killed out-of-band (see
// invalidateSession), leaving the client holding a dead token. Without the
// option the next request surfaces ErrInvalidToken (host code 952); with it, the
// client re-authenticates and retries transparently, rotating to a fresh token.
func TestIntegrationReauthOnInvalidToken(t *testing.T) {
	requireLayout(t)
	ctx := context.Background()
	// A query that matches nothing keeps the request cheap and side-effect free.
	emptyQuery := []FindRequest{{Criteria: map[string]string{fieldText: "==go-filemaker-it-never-matches"}}}

	// Control: no reauth option, so a dead token surfaces as ErrInvalidToken.
	// This also confirms the out-of-band logout genuinely invalidated the token.
	plain, err := New(itHost, itDatabase, itUsername, itPassword, itBaseOptions()...)
	if err != nil {
		t.Fatalf("New (plain): %v", err)
	}
	if err := plain.Authenticate(ctx); err != nil {
		t.Fatalf("Authenticate (plain): %v", err)
	}
	t.Cleanup(func() { _ = plain.Logout(context.Background()) })

	invalidateSession(t, plain)
	if _, err := plain.Find(ctx, itLayout, emptyQuery); !errors.Is(err, ErrInvalidToken) {
		t.Errorf("Find with dead token (no reauth) error = %v, want ErrInvalidToken", err)
	}

	// With WithReauthOnInvalidToken (via itClientOptions), the same dead token is
	// recovered transparently and the client rotates to a new token.
	reauth := buildITClient(t)
	dead := invalidateSession(t, reauth)
	res, err := reauth.Find(ctx, itLayout, emptyQuery)
	if err != nil {
		t.Fatalf("Find with dead token (reauth) should recover, got: %v", err)
	}
	if res.Records == nil {
		t.Error("expected empty (non-nil) records after recovery")
	}
	reauth.mu.RLock()
	newToken := reauth.token
	reauth.mu.RUnlock()
	if newToken == "" || newToken == dead {
		t.Errorf("token after reauth = %q, want a fresh non-empty token (was %q)", newToken, dead)
	}
}

// TestIntegrationTimeOfDay round-trips a FileMaker Time field both ways: a clock
// value written via the Time wrapper and read back with Time()/Duration(), then
// an elapsed duration exceeding 24h written via the Duration wrapper and read
// back with Duration().
func TestIntegrationTimeOfDay(t *testing.T) {
	requireLayout(t)
	ctx := context.Background()

	marker := "go-filemaker-it-tod-" + time.Now().UTC().Format("20060102T150405.000000000")
	created, err := itClient.Create(ctx, itLayout, FieldData{
		fieldText:     marker,
		fieldTime:     Time(time.Date(2025, 6, 23, 15, 4, 5, 0, time.UTC)),
		fieldRequired: "present",
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	t.Cleanup(func() {
		if _, err := itClient.DeleteByID(context.Background(), itLayout, created.RecordID); err != nil {
			t.Errorf("cleanup DeleteByID(%s): %v", created.RecordID, err)
		}
	})

	rec := findParent(t, ctx, marker)
	if got := rec.Time(fieldTime).Format("15:04:05"); got != "15:04:05" {
		t.Errorf("Time(%s) clock = %s, want 15:04:05", fieldTime, got)
	}
	if got, want := rec.Duration(fieldTime), 15*time.Hour+4*time.Minute+5*time.Second; got != want {
		t.Errorf("Duration(%s) = %v, want %v", fieldTime, got, want)
	}

	// An elapsed duration over 24h, via the Duration wrapper.
	if _, err := itClient.UpdateByID(ctx, itLayout, created.RecordID, FieldData{
		fieldTime: Duration(37*time.Hour + 30*time.Minute),
	}); err != nil {
		t.Fatalf("UpdateByID: %v", err)
	}
	rec = findParent(t, ctx, marker)
	if got, want := rec.Duration(fieldTime), 37*time.Hour+30*time.Minute; got != want {
		t.Errorf("Duration(%s) after update = %v, want %v", fieldTime, got, want)
	}
}

// TestIntegrationWithDateFormatISO exercises WithDateFormat against a real host.
// A sibling client configured for ISO writes the date in ISO 8601 and sends
// dateformats=2; the create succeeding is itself the proof that the parameter
// was applied, since ISO date strings without it are rejected (error 500). The
// stored value is then confirmed unchanged by reading it back through the
// default (US) client.
func TestIntegrationWithDateFormatISO(t *testing.T) {
	requireLayout(t)
	ctx := context.Background()

	c := buildITClient(t, WithDateFormat(DateFormatISO))

	marker := "go-filemaker-it-iso-" + time.Now().UTC().Format("20060102T150405.000000000")
	date := time.Date(2025, 6, 23, 0, 0, 0, 0, time.UTC)
	ts := time.Date(2025, 6, 23, 15, 4, 5, 0, time.UTC)

	created, err := c.Create(ctx, itLayout, FieldData{
		fieldText:      marker,
		fieldDate:      Date(date),
		fieldTimestamp: Timestamp(ts),
		fieldRequired:  "present",
	})
	if err != nil {
		t.Fatalf("Create with WithDateFormat(ISO): %v", err)
	}
	t.Cleanup(func() {
		if _, err := itClient.DeleteByID(context.Background(), itLayout, created.RecordID); err != nil {
			t.Errorf("cleanup DeleteByID(%s): %v", created.RecordID, err)
		}
	})

	// Read back through the default client: the value is unchanged by the write
	// format (both parse to the same instant in UTC).
	rec := findParent(t, ctx, marker)
	if got, err := rec.TimeE(fieldDate); err != nil || !got.Equal(date) {
		t.Errorf("DateField round-trip = %v (err %v), want %v", got, err, date)
	}
	if got, err := rec.TimeE(fieldTimestamp); err != nil || !got.Equal(ts) {
		t.Errorf("TimestampField round-trip = %v (err %v), want %v", got, err, ts)
	}

	// Edit also carries dateformats=2: confirm the host accepts ISO input on the
	// PATCH endpoint, not just create. Like the create, success is the proof —
	// an ISO date without the parameter would be rejected (error 500).
	newDate := time.Date(2024, 12, 25, 0, 0, 0, 0, time.UTC)
	newTS := time.Date(2024, 12, 25, 8, 30, 0, 0, time.UTC)
	if _, err := c.UpdateByID(ctx, itLayout, created.RecordID, FieldData{
		fieldDate:      Date(newDate),
		fieldTimestamp: Timestamp(newTS),
	}); err != nil {
		t.Fatalf("UpdateByID with WithDateFormat(ISO): %v", err)
	}
	rec = findParent(t, ctx, marker)
	if got, err := rec.TimeE(fieldDate); err != nil || !got.Equal(newDate) {
		t.Errorf("DateField after ISO edit = %v (err %v), want %v", got, err, newDate)
	}
	if got, err := rec.TimeE(fieldTimestamp); err != nil || !got.Equal(newTS) {
		t.Errorf("TimestampField after ISO edit = %v (err %v), want %v", got, err, newTS)
	}
}
