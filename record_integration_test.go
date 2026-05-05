package filemaker

import (
	"os"
	"testing"
	"time"
)

// integrationSession returns a live session for integration tests.
// Skips the test if the required environment variables are not set:
//
//	FM_TEST_HOST, FM_TEST_DB, FM_TEST_USER, FM_TEST_PASS,
//	FM_TEST_LAYOUT, FM_TEST_FIELD (a writable text field on the layout)
func integrationSession(t *testing.T) (*Session, string, string) {
	t.Helper()
	host := os.Getenv("FM_TEST_HOST")
	db := os.Getenv("FM_TEST_DB")
	user := os.Getenv("FM_TEST_USER")
	pass, passSet := os.LookupEnv("FM_TEST_PASS")
	layout := os.Getenv("FM_TEST_LAYOUT")
	field := os.Getenv("FM_TEST_FIELD")
	if host == "" || db == "" || user == "" || !passSet || layout == "" || field == "" {
		t.Skip("integration test skipped: set FM_TEST_HOST, FM_TEST_DB, FM_TEST_USER, FM_TEST_PASS, FM_TEST_LAYOUT, FM_TEST_FIELD")
	}
	fm, err := New(host, db, user, pass)
	if err != nil {
		t.Fatalf("filemaker.New: %v", err)
	}
	t.Cleanup(func() { _ = fm.Destroy() })
	return fm, layout, field
}

// TestModID_PopulatedOnCreate verifies that a freshly created record
// receives a modId from the Data API.
func TestModID_PopulatedOnCreate(t *testing.T) {
	fm, layout, field := integrationSession(t)

	record := fm.NewRecord(layout)
	record.Set(field, "modid-create-"+time.Now().Format(time.RFC3339Nano))
	if err := record.Commit(); err != nil {
		t.Fatalf("Commit (create): %v", err)
	}
	t.Cleanup(func() { _ = record.Delete() })

	if record.ModID == "" {
		t.Errorf("expected ModID to be populated after create, got empty string")
	}
}

// TestModID_RefreshedOnEdit verifies that ModID advances after a successful edit
// so that a subsequent commit on the same record uses the new value.
func TestModID_RefreshedOnEdit(t *testing.T) {
	fm, layout, field := integrationSession(t)
	fm.UseModID = true

	record := fm.NewRecord(layout)
	record.Set(field, "modid-edit-"+time.Now().Format(time.RFC3339Nano))
	if err := record.Commit(); err != nil {
		t.Fatalf("Commit (create): %v", err)
	}
	t.Cleanup(func() { _ = record.Delete() })

	before := record.ModID
	if before == "" {
		t.Fatalf("expected ModID populated after create")
	}

	record.Set(field, "modid-edit-after-"+time.Now().Format(time.RFC3339Nano))
	if err := record.Commit(); err != nil {
		t.Fatalf("Commit (edit) with current modId should succeed: %v", err)
	}

	if record.ModID == before {
		t.Errorf("expected ModID to change after edit, still %q", record.ModID)
	}
}

// TestModID_RejectsStaleEdit verifies that the server rejects an edit
// committed with a stale modId (i.e. the record was updated by someone else
// between read and write).
func TestModID_RejectsStaleEdit(t *testing.T) {
	fm, layout, field := integrationSession(t)
	fm.UseModID = true

	marker := "modid-stale-" + time.Now().Format(time.RFC3339Nano)

	// Create the record with a unique marker so we can find it.
	seed := fm.NewRecord(layout)
	seed.Set(field, marker)
	if err := seed.Commit(); err != nil {
		t.Fatalf("Commit (create): %v", err)
	}
	t.Cleanup(func() { _ = seed.Delete() })

	// Fetch the same row into two independent Record values.
	findByMarker := func() Record {
		recs, err := fm.Find(layout, NewFindCommand(
			NewFindRequest(NewFindCriterion(field, "=="+marker)),
		))
		if err != nil {
			t.Fatalf("Find: %v", err)
		}
		if len(recs) != 1 {
			t.Fatalf("expected 1 record matching marker, got %d", len(recs))
		}
		return recs[0]
	}
	copyA := findByMarker()
	copyB := findByMarker()

	if copyA.ModID == "" || copyB.ModID == "" {
		t.Fatalf("expected ModID populated on both copies, got A=%q B=%q", copyA.ModID, copyB.ModID)
	}
	if copyA.ModID != copyB.ModID {
		t.Fatalf("expected both copies to have the same starting ModID, got A=%q B=%q", copyA.ModID, copyB.ModID)
	}

	// Edit copy A successfully — bumps the server's modId.
	copyA.Set(field, marker+"-A")
	if err := copyA.Commit(); err != nil {
		t.Fatalf("Commit (copyA, fresh modId) should succeed: %v", err)
	}

	// Edit copy B with its now-stale modId — server should reject.
	copyB.Set(field, marker+"-B")
	if err := copyB.Commit(); err == nil {
		t.Errorf("expected stale-modId Commit on copyB to be rejected, got nil error")
	}
}
