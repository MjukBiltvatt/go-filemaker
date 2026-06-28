package filemaker

import (
	"errors"
	"net/url"
)

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

// DeleteOption configures a Delete or DeleteByID.
type DeleteOption interface{ applyDelete(*recordConfig) }

// FindOption configures a Find.
type FindOption interface{ applyFind(*recordConfig) }

// WriteOption configures any record write: it is accepted by both Create and
// Update (and UpdateByID).
type WriteOption interface {
	CreateOption
	UpdateOption
}

// RecordOption configures any record-context endpoint that can run scripts with
// the request. It is accepted by Create, Update (and UpdateByID), and Delete (and
// DeleteByID).
type RecordOption interface {
	CreateOption
	UpdateOption
	DeleteOption
}

// ReadManyOption configures a read that returns multiple records — sorting and
// paging. It is accepted by Find (and, in future, the get-range endpoint).
type ReadManyOption interface {
	FindOption
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

	// Scripts run with the request: script after the action, prerequest before
	// the request is processed, presort after the action but before the sort.
	script     scriptCall
	prerequest scriptCall
	presort    scriptCall

	// Read shaping for finds (and, later, the get-range endpoint).
	sort   []SortRule
	limit  int
	offset int

	err error
}

// scriptCall is a script to run with a request: its name and an optional
// parameter. A zero scriptCall (empty name) means no script for that phase.
type scriptCall struct {
	name  string
	param string
}

// scriptParams returns the script-directive wire key/value pairs the config
// carries, in a stable order, with empty phases (and empty params) omitted. The
// keys are identical for the JSON body (Create/Update) and the URL query string
// (Delete), so both serializers draw from here.
func (c recordConfig) scriptParams() [][2]string {
	var out [][2]string
	add := func(key string, s scriptCall) {
		if s.name == "" {
			return
		}
		out = append(out, [2]string{key, s.name})
		if s.param != "" {
			out = append(out, [2]string{key + ".param", s.param})
		}
	}
	add("script", c.script)
	add("script.prerequest", c.prerequest)
	add("script.presort", c.presort)
	return out
}

// queryParams returns the parameters that ride in the URL query string rather
// than a request body — used by Delete, which has no body. Currently that is the
// script directives, whose names match their body keys.
func (c recordConfig) queryParams() url.Values {
	v := url.Values{}
	for _, kv := range c.scriptParams() {
		v.Set(kv[0], kv[1])
	}
	return v
}

// option is the single adapter behind every option constructor: a closure that
// mutates the shared recordConfig. It implements every endpoint's apply method,
// so a constructor's scope is governed entirely by the interface type it is
// returned as — not by the methods this concrete type happens to carry.
type option func(*recordConfig)

func (o option) applyCreate(c *recordConfig) { o(c) }
func (o option) applyUpdate(c *recordConfig) { o(c) }
func (o option) applyDelete(c *recordConfig) { o(c) }
func (o option) applyFind(c *recordConfig)   { o(c) }

// WithModID makes the update conditional (optimistic concurrency) against a
// specific mod ID: the host rejects it with ErrRecordModified if the record's
// current mod ID differs — i.e. it changed since modID was read. modID must be
// non-empty; an empty one is reported as an error from Update/UpdateByID. To
// lock against the record you are updating, prefer IfUnchanged.
//
// It sets a single mod ID; calling WithModID again keeps only the last value.
// (Combining it with IfUnchanged is a documented exception, not a duplicate: the
// explicit version from WithModID is used regardless of order.)
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
//
// It sets a single portal-data object; calling WithPortalData again replaces it
// rather than merging — pass all the portals and rows in one call.
func WithPortalData(portals PortalData) WriteOption {
	return option(func(c *recordConfig) {
		c.portalData = portals
	})
}

// WithScript runs a FileMaker script after the request's action completes,
// passing param as its script parameter (pass "" for none). The script runs in
// the layout's context. It is accepted by Create, Update, and Delete.
//
// The Data API runs at most one script per phase, so this sets a single script:
// calling WithScript more than once keeps only the last. The three phases
// (WithScript, WithPrerequestScript, WithPresortScript) are independent and
// compose; to run several steps in one phase, chain them inside a single
// FileMaker script.
func WithScript(name, param string) RecordOption {
	return option(func(c *recordConfig) {
		c.script = scriptCall{name, param}
	})
}

// WithPrerequestScript runs a script before the request is processed — the Data
// API script.prerequest — passing param as its parameter (pass "" for none). It
// is accepted by Create, Update, and Delete. Like WithScript it sets a single
// script; calling it again keeps only the last.
func WithPrerequestScript(name, param string) RecordOption {
	return option(func(c *recordConfig) {
		c.prerequest = scriptCall{name, param}
	})
}

// WithPresortScript runs a script after the request's action but before the
// result is sorted — the Data API script.presort — passing param as its
// parameter (pass "" for none). The presort phase is most meaningful for reads;
// the endpoint accepts it regardless. It is accepted by Create, Update, and
// Delete. Like WithScript it sets a single script; calling it again keeps only
// the last.
func WithPresortScript(name, param string) RecordOption {
	return option(func(c *recordConfig) {
		c.presort = scriptCall{name, param}
	})
}

// WithSort orders a find result by each rule in turn. Calling it again replaces
// the previous rules; passing no rules leaves the result unsorted.
func WithSort(rules ...SortRule) ReadManyOption {
	return option(func(c *recordConfig) {
		c.sort = rules
	})
}

// WithLimit caps the number of records a find returns (the host's default is
// 100). A non-positive limit is treated as unset, leaving the host default.
func WithLimit(limit int) ReadManyOption {
	return option(func(c *recordConfig) {
		c.limit = limit
	})
}

// WithOffset sets the 1-based index of the first record a find returns (the
// host's default is 1). A non-positive offset is treated as unset.
func WithOffset(offset int) ReadManyOption {
	return option(func(c *recordConfig) {
		c.offset = offset
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

// resolveDeleteConfig applies the delete options. Deferred option errors surface
// here.
func resolveDeleteConfig(opts []DeleteOption) (recordConfig, error) {
	var cfg recordConfig
	for _, opt := range opts {
		if opt != nil {
			opt.applyDelete(&cfg)
		}
	}
	return cfg, cfg.err
}

// resolveFindConfig applies the find options. Deferred option errors surface
// here.
func resolveFindConfig(opts []FindOption) (recordConfig, error) {
	var cfg recordConfig
	for _, opt := range opts {
		if opt != nil {
			opt.applyFind(&cfg)
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
