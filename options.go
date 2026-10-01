package filemaker

import (
	"errors"
	"fmt"
	"sort"
)

// This file defines the option type lattice shared by the record-writing
// endpoints.
//
// Each endpoint takes its options through a sealed interface (CreateOption,
// UpdateOption, …). The interfaces are sealed: their apply methods are
// unexported and take an unexported *params, so only this package can
// produce values that satisfy them. That keeps each endpoint's set of valid
// options closed, and lets the lattice grow — new endpoints, new options — with
// only additive (non-breaking) changes.
//
// An option valid on more than one endpoint returns a "breadth" interface that
// embeds each endpoint's interface (e.g. WriteOption embeds both CreateOption
// and UpdateOption). One constructor then flows into every endpoint it is valid
// for, while the compiler still rejects it everywhere it is not.

// CreateOption configures a Create.
type CreateOption interface{ applyCreate(*params) }

// UpdateOption configures an Update or UpdateByID.
type UpdateOption interface{ applyUpdate(*params) }

// DeleteOption configures a Delete or DeleteByID.
type DeleteOption interface{ applyDelete(*params) }

// DuplicateOption configures a Duplicate or DuplicateByID.
type DuplicateOption interface{ applyDuplicate(*params) }

// FindOption configures a Find.
type FindOption interface{ applyFind(*params) }

// GetOption configures a Get or GetByID.
type GetOption interface{ applyGet(*params) }

// GetRangeOption configures a GetRange.
type GetRangeOption interface{ applyGetRange(*params) }

// UploadOption configures an UploadToContainer or UploadToContainerByID.
type UploadOption interface{ applyUpload(*params) }

// WriteOption configures any record write: it is accepted by both Create and
// Update (and UpdateByID).
type WriteOption interface {
	CreateOption
	UpdateOption
}

// ConcurrencyOption configures optimistic concurrency on a write to an existing
// record. It is accepted by Update (and UpdateByID) and UploadToContainer (and
// UploadToContainerByID) — the two endpoints that mutate an existing record and
// support a mod-ID check.
type ConcurrencyOption interface {
	UpdateOption
	UploadOption
}

// RecordOption configures any record-context endpoint that can run scripts with
// the request. It is accepted by Create, Update (and UpdateByID), Delete (and
// DeleteByID), Duplicate (and DuplicateByID), Find, Get (and GetByID), and
// GetRange.
type RecordOption interface {
	CreateOption
	UpdateOption
	DeleteOption
	DuplicateOption
	FindOption
	GetOption
	GetRangeOption
}

// ReadManyOption configures a read that returns multiple records — sorting and
// paging. It is accepted by Find and GetRange.
type ReadManyOption interface {
	FindOption
	GetRangeOption
}

// ReadOption configures how a read returns records: which portals to include,
// how to page them, and the layout to return data in. It is accepted by Find,
// Get (and GetByID), and GetRange.
type ReadOption interface {
	FindOption
	GetOption
	GetRangeOption
}

// EntryMode selects whether the host applies a field's rules to a write the way
// interactive data entry would ("user", the default) or the way a server-side
// script does, bypassing them ("script"). It is the value type for WithEntryMode
// (validation) and WithProhibitMode (automatic data entry).
type EntryMode string

const (
	// EntryModeUser follows the field's rules (the host default).
	EntryModeUser EntryMode = "user"
	// EntryModeScript ignores the field's rules.
	EntryModeScript EntryMode = "script"
)

// SortOrder is the direction of a SortRule.
type SortOrder string

const (
	// SortAscending sorts the result in ascending order.
	SortAscending SortOrder = "ascend"
	// SortDescending sorts the result in descending order.
	SortDescending SortOrder = "descend"
)

// SortRule sorts the result by a field in the given order. It is the value type
// for WithSort, accepted by Find and GetRange. It maps 1:1 to the Data API sort
// object, so JSON tags carry the wire names directly.
type SortRule struct {
	Field string    `json:"fieldName"`
	Order SortOrder `json:"sortOrder"`
}

// Asc returns a SortRule that sorts by field in ascending order.
func Asc(field string) SortRule { return SortRule{Field: field, Order: SortAscending} }

// Desc returns a SortRule that sorts by field in descending order.
func Desc(field string) SortRule { return SortRule{Field: field, Order: SortDescending} }

// option is the single adapter behind every option constructor: a closure that
// mutates the shared params. It implements every endpoint's apply method,
// so a constructor's scope is governed entirely by the interface type it is
// returned as — not by the methods this concrete type happens to carry.
type option func(*params)

func (o option) applyCreate(c *params)    { o(c) }
func (o option) applyUpdate(c *params)    { o(c) }
func (o option) applyDelete(c *params)    { o(c) }
func (o option) applyDuplicate(c *params) { o(c) }
func (o option) applyFind(c *params)      { o(c) }
func (o option) applyGet(c *params)       { o(c) }
func (o option) applyGetRange(c *params)  { o(c) }
func (o option) applyUpload(c *params)    { o(c) }

// WithModID makes the write conditional (optimistic concurrency) against a
// specific mod ID: the host rejects it with ErrRecordModified if the record's
// current mod ID differs — i.e. it changed since modID was read. modID must be
// non-empty; an empty one is reported as an error from the call. To lock against
// the record you are writing, prefer IfUnchanged. Accepted by Update and
// UploadToContainer.
//
// It sets a single mod ID; calling WithModID again keeps only the last value.
// (Combining it with IfUnchanged is a documented exception, not a duplicate: the
// explicit version from WithModID is used regardless of order.)
//
// It locks the record only. Portal rows written with WithPortalData are separate
// related records with their own mod IDs, and each is written unconditionally
// unless its PortalRowData carries a ModID.
func WithModID(modID string) ConcurrencyOption {
	return option(func(c *params) {
		if modID == "" {
			c.err = errors.New("filemaker: WithModID requires a non-empty mod ID")
			return
		}
		c.conditional = true
		c.modID = modID
	})
}

// IfUnchanged makes the write conditional on the record not having changed since
// it was read: it locks against the record's own ModID, so the host rejects the
// write with ErrRecordModified if another writer modified the record in the
// meantime. It is the ergonomic form of WithModID(rec.ModID()). Accepted by
// Update and UploadToContainer.
//
// Only the record-based forms (Update, UploadToContainer) can honor it; the
// *ByID forms have no record to read a ModID from and report an error. A record
// without a ModID is likewise an error rather than a silent unconditional write.
// Combining it with WithModID is redundant — the explicit version from WithModID
// is used, regardless of order.
//
// It locks the record only. Portal rows written with WithPortalData are separate
// related records with their own mod IDs, and each is written unconditionally
// unless its PortalRowData carries a ModID.
func IfUnchanged() ConcurrencyOption {
	return option(func(c *params) {
		c.conditional = true
	})
}

// PortalData is the related-record data WithPortalData writes: a portal name
// mapped to the rows to write through it. A portal is named as Record.Portal
// names it — by its object name when it has one, otherwise by its
// table-occurrence name. It mirrors the Data API's portalData object.
type PortalData map[string][]PortalRowData

// PortalRowData is one row of PortalData: a related record to add or edit
// through a portal. Fields holds its values keyed by fully qualified name
// ("TableOccurrence::FieldName"), with the same value types FieldData accepts.
//
// An empty ID adds a new related record; a non-empty ID edits the existing
// related record with that ID, and only on an Update. ModID makes that edit
// conditional, like WithModID does for the record: the host rejects the write
// with ErrRecordModified if the related record changed since that mod ID was
// read. It requires an ID. To edit a row read through Record.Portal, copy its
// PortalRow.ID, and PortalRow.ModID to lock it.
//
// Unlike a record, whose ID is part of the request URL, a related record is
// addressed inside the request body, so its ID travels with its values here.
type PortalRowData struct {
	ID     string
	ModID  string
	Fields FieldData
}

// check reports the first row that could not be sent as written: one with a
// ModID but no ID, or one whose Fields carries the host's own "recordId" or
// "modId" key, which ID and ModID set. create reports a row with an ID, which a
// Create cannot edit.
func (pd PortalData) check(create bool) error {
	// Portals in name order, so a request with several faulty rows always
	// reports the same one.
	portals := make([]string, 0, len(pd))
	for portal := range pd {
		portals = append(portals, portal)
	}
	sort.Strings(portals)
	for _, portal := range portals {
		for i, row := range pd[portal] {
			switch {
			case create && row.ID != "":
				return fmt.Errorf("filemaker: portal %q row %d has ID %q, but Create adds every portal row as a new related record", portal, i, row.ID)
			case row.ModID != "" && row.ID == "":
				return fmt.Errorf("filemaker: portal %q row %d has a ModID but no ID; only an edit of an existing related record can be conditional", portal, i)
			}
			for _, key := range []string{"recordId", "modId"} {
				if _, ok := row.Fields[key]; ok {
					return fmt.Errorf("filemaker: portal %q row %d has a %q field; set PortalRowData.ID and ModID instead", portal, i, key)
				}
			}
		}
	}
	return nil
}

// WithPortalData attaches related-record data to a Create or Update, applied by
// the host alongside the field-data patch (see PortalData and PortalRowData).
// On a Create every row is added as a new related record, so a row with an ID
// is an error. On an Update a row with an ID edits that related record, and a
// row without one adds a new related record.
//
// Only the listed rows are touched; rows you omit are left unchanged. To
// remove related records, set "deleteRelated" in the FieldData patch to
// "TableOccurrence.recordId" (e.g. "Orders.3", or a slice for several): it is a
// field-data directive, not a portal edit. It names the table occurrence the
// portal shows (PortalDataInfo.Table), even for a portal with an object name,
// whose name the host refuses there with code 110 ("Related tables are
// missing"). To edit only portals and leave the record's own fields untouched,
// pass a nil or empty FieldData. See the Claris Data API guide's "Edit record"
// page.
//
// It sets a single portal-data object; calling WithPortalData again replaces it
// rather than merging — pass all the portals and rows in one call.
func WithPortalData(portals PortalData) WriteOption {
	return option(func(c *params) {
		if err := portals.check(false); err != nil {
			c.err = err
			return
		}
		c.portalData = portals
	})
}

// WithEntryMode sets whether the write honors field data validation — the Data
// API options.entrymode. EntryModeUser (the default) follows each field's
// validation requirements; EntryModeScript ignores them. Accepted by Create and
// Update.
func WithEntryMode(mode EntryMode) WriteOption {
	return option(func(c *params) {
		c.entryMode = mode
	})
}

// WithProhibitMode sets whether the write honors field automatic data entry —
// the Data API options.prohibitmode. EntryModeUser (the default) follows each
// field's auto-enter requirements; EntryModeScript ignores them. Accepted by
// Create and Update.
func WithProhibitMode(mode EntryMode) WriteOption {
	return option(func(c *params) {
		c.prohibitMode = mode
	})
}

// WithScript runs a FileMaker script after the request's action completes,
// passing param as its script parameter (pass "" for none). The script runs in
// the layout's context. It is accepted by every endpoint that takes a
// RecordOption; the outcome is reported in the corresponding response's Scripts
// field.
//
// The Data API runs at most one script per phase, so this sets a single script:
// calling WithScript more than once keeps only the last. The three phases
// (WithScript, WithPrerequestScript, WithPresortScript) are independent and
// compose; to run several steps in one phase, chain them inside a single
// FileMaker script.
func WithScript(name, param string) RecordOption {
	return option(func(c *params) {
		c.script = scriptCall{name, param}
	})
}

// WithPrerequestScript runs a script before the request is processed — the Data
// API script.prerequest — passing param as its parameter (pass "" for none). It
// is accepted by every endpoint that takes a RecordOption. Like WithScript it
// sets a single script; calling it again keeps only the last.
func WithPrerequestScript(name, param string) RecordOption {
	return option(func(c *params) {
		c.prerequest = scriptCall{name, param}
	})
}

// WithPresortScript runs a script after the request's action but before the
// result is sorted — the Data API script.presort — passing param as its
// parameter (pass "" for none). The presort phase is most meaningful on Find,
// where it can shape the found set before sorting; the other endpoints accept it
// regardless. It is accepted by every endpoint that takes a RecordOption. Like
// WithScript it sets a single script; calling it again keeps only the last.
func WithPresortScript(name, param string) RecordOption {
	return option(func(c *params) {
		c.presort = scriptCall{name, param}
	})
}

// WithSort orders the records a read returns by each rule in turn. Accepted by
// Find and GetRange. Calling it again replaces the previous rules; passing no
// rules leaves the result unsorted.
func WithSort(rules ...SortRule) ReadManyOption {
	return option(func(c *params) {
		c.sort = rules
	})
}

// WithLimit caps the number of records a read returns (the host's default is
// 100). Accepted by Find and GetRange. A non-positive limit is treated as unset,
// leaving the host default.
func WithLimit(limit int) ReadManyOption {
	return option(func(c *params) {
		c.limit = limit
	})
}

// WithOffset sets the 1-based index of the first record a read returns (the
// host's default is 1). Accepted by Find and GetRange. A non-positive offset is
// treated as unset.
func WithOffset(offset int) ReadManyOption {
	return option(func(c *params) {
		c.offset = offset
	})
}

// WithResponseLayout returns each record's data in the context of layout rather
// than the one the read targets — the Data API layout.response. The two layouts
// must share a base table. Calling it again keeps only the last.
func WithResponseLayout(layout string) ReadOption {
	return option(func(c *params) {
		c.responseLayout = layout
	})
}

// WithPortals restricts which portals the result includes to the named ones;
// portals not listed are omitted from both Record.Portal and
// Record.PortalDataInfo. A portal is named by its object name when it has one,
// otherwise by its table-occurrence name — a portal with an object name does not
// answer to its table occurrence. A name that matches no portal on the layout
// fails the request (FileMaker error 110). The record's own field data is always
// returned — this affects only which portals accompany it, not whether field
// data comes back. Omitting the option (or passing no names) returns all
// portals. Calling it again replaces the set.
func WithPortals(names ...string) ReadOption {
	return option(func(c *params) {
		c.portals = names
	})
}

// WithPortalLimit caps the number of related records returned for the named
// portal. Without it the host caps them at the layout portal's configured row
// count (and at most its documented default of 50), so a portal can come back
// with fewer rows than actually exist; pass this to raise (or lower) that cap. A
// non-positive limit is treated as unset. Calling it again for the same portal
// keeps only the last.
func WithPortalLimit(portal string, limit int) ReadOption {
	return option(func(c *params) {
		c.updatePortalRange(portal, func(pr *portalRange) { pr.limit = limit })
	})
}

// WithPortalOffset sets the 1-based index of the first related record returned
// for the named portal (the host's default is 1). A non-positive offset is
// treated as unset. Calling it again for the same portal keeps only the last.
func WithPortalOffset(portal string, offset int) ReadOption {
	return option(func(c *params) {
		c.updatePortalRange(portal, func(pr *portalRange) { pr.offset = offset })
	})
}
