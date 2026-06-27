package filemaker

import "errors"

// This file defines the option type lattice shared by the record-writing
// endpoints.
//
// Each endpoint takes its options through a sealed interface (CreateOption,
// UpdateOption, …). The interfaces are sealed: their apply methods are
// unexported and take an unexported *recordConfig, so only this package can
// produce values that satisfy them. That keeps each endpoint's set of valid
// options closed, and lets the lattice grow — new endpoints, new options — with
// only additive (non-breaking) changes.
//
// An option valid on more than one endpoint returns a "breadth" interface that
// embeds each endpoint's interface (e.g. WriteOption embeds both CreateOption
// and UpdateOption). One constructor then flows into every endpoint it is valid
// for, while the compiler still rejects it everywhere it is not.

// CreateOption configures a Create.
type CreateOption interface{ applyCreate(*recordConfig) }

// UpdateOption configures an Update or UpdateByID.
type UpdateOption interface{ applyUpdate(*recordConfig) }

// WriteOption configures any record write: it is accepted by both Create and
// Update (and UpdateByID).
type WriteOption interface {
	CreateOption
	UpdateOption
}

// recordConfig accumulates the optional parameters the options set; each
// endpoint resolves it into a request. Optimistic concurrency is one flag plus a
// version: conditional means a mod-ID check is wanted, and modID is the version
// to check against (empty means "source it from the record"). Both WithModID and
// IfUnchanged set conditional, so the options are order-independent. err carries
// deferred option validation (an option cannot return an error directly),
// surfaced when the config is resolved.
type recordConfig struct {
	conditional bool
	modID       string
	portalData  PortalData
	err         error
}

// option is the single adapter behind every option constructor: a closure that
// mutates the shared recordConfig. It implements every endpoint's apply method,
// so a constructor's scope is governed entirely by the interface type it is
// returned as — not by the methods this concrete type happens to carry.
type option func(*recordConfig)

func (o option) applyCreate(c *recordConfig) { o(c) }
func (o option) applyUpdate(c *recordConfig) { o(c) }

// WithModID makes the update conditional (optimistic concurrency) against a
// specific mod ID: the host rejects it with ErrRecordModified if the record's
// current mod ID differs — i.e. it changed since modID was read. modID must be
// non-empty; an empty one is reported as an error from Update/UpdateByID. To
// lock against the record you are updating, prefer IfUnchanged.
func WithModID(modID string) UpdateOption {
	return option(func(c *recordConfig) {
		if modID == "" {
			c.err = errors.New("filemaker: WithModID requires a non-empty mod ID")
			return
		}
		c.conditional = true
		c.modID = modID
	})
}

// IfUnchanged makes the update conditional on the record not having changed
// since it was read: it locks against the record's own ModID, so the host
// rejects the write with ErrRecordModified if another writer modified the record
// in the meantime. It is the ergonomic form of WithModID(rec.ModID()).
//
// Only the record-based Update can honor it (UpdateByID has no record to read a
// ModID from, and reports an error); a record without a ModID is likewise an
// error rather than a silent unconditional write. Combining it with WithModID is
// redundant — the explicit version from WithModID is used, regardless of order.
func IfUnchanged() UpdateOption {
	return option(func(c *recordConfig) {
		c.conditional = true
	})
}

// WithPortalData attaches related-record data to a Create or Update, applied by
// the host alongside the field-data patch. Pass the rows in the same shape
// Record.Portals returns: a portal name mapped to its rows, each row's field
// values keyed by fully qualified name ("TableOccurrence::FieldName").
//
// On a Create, every row is added as a new related record. On an Update a row
// carrying a record ID (a plain "recordId" key) edits that existing related
// record — add a plain "modId" for optimistic locking — and a row without one is
// added as a new related record. Note the record ID is not table-occurrence
// qualified like the field values are: the host reads "TableOccurrence::recordId"
// as a field and rejects the edit with code 102 ("Field is missing").
//
// Only the named portal rows are touched; rows you omit are left unchanged. To
// remove related records, set "deleteRelated" in the FieldData patch (e.g.
// "Orders.3", or a slice for several): it is a field-data directive, not a portal
// edit. To edit only portals and leave the record's own fields untouched, pass a
// nil or empty FieldData. See the Claris Data API guide's "Edit record" page.
func WithPortalData(portals PortalData) WriteOption {
	return option(func(c *recordConfig) {
		c.portalData = portals
	})
}

// resolveCreateConfig applies the create options. Deferred option errors surface
// here.
func resolveCreateConfig(opts []CreateOption) (recordConfig, error) {
	var cfg recordConfig
	for _, opt := range opts {
		if opt != nil {
			opt.applyCreate(&cfg)
		}
	}
	return cfg, cfg.err
}

// resolveUpdateConfig applies the options and resolves the mod ID. A conditional
// update with no explicit version sources it from rec (nil for the id-addressed
// path, which cannot honor IfUnchanged). Deferred option errors surface here.
func resolveUpdateConfig(opts []UpdateOption, rec *Record) (recordConfig, error) {
	var cfg recordConfig
	for _, opt := range opts {
		if opt != nil {
			opt.applyUpdate(&cfg)
		}
	}
	if cfg.err != nil {
		return cfg, cfg.err
	}
	if cfg.conditional && cfg.modID == "" {
		switch {
		case rec == nil:
			return cfg, errors.New("filemaker: IfUnchanged requires a record; use WithModID with UpdateByID")
		case rec.modID == "":
			return cfg, errors.New("filemaker: IfUnchanged requires a record with a ModID")
		}
		cfg.modID = rec.modID
	}
	return cfg, nil
}
