package filemaker

import (
	"errors"
	"fmt"
	"net/url"
)

// params accumulates the optional parameters the options set; each endpoint
// resolves it into a request. Optimistic concurrency is one flag plus a version:
// conditional means a mod-ID check is wanted, and modID is the version to check
// against (empty means "source it from the record"). Both WithModID and
// IfUnchanged set conditional, so the options are order-independent. err carries
// deferred option validation (an option cannot return an error directly),
// surfaced when the params are resolved.
type params struct {
	conditional bool
	modID       string
	portalData  PortalData

	// Data-entry behavior on Create/Update (the body "options" object).
	entryMode    EntryMode
	prohibitMode EntryMode

	// Scripts run with the request: script after the action, prerequest before
	// the request is processed, presort after the action but before the sort.
	script     scriptCall
	prerequest scriptCall
	presort    scriptCall

	// Read shaping for finds (and, later, the get endpoints). responseLayout maps
	// to layout.response; portals selects which portals to return; portalRanges
	// pages within a named portal (offset.<name>/limit.<name>).
	sort           []SortRule
	limit          int
	offset         int
	responseLayout string
	portals        []string
	portalRanges   map[string]portalRange

	err error
}

// portalRange is the offset/limit paging for a single named portal. A zero field
// means unset for that dimension.
type portalRange struct {
	offset int
	limit  int
}

// scriptCall is a script to run with a request: its name and an optional
// parameter. A zero scriptCall (empty name) means no script for that phase.
type scriptCall struct {
	name  string
	param string
}

// updatePortalRange applies fn to the range for the named portal, creating the
// map and entry on first use. Map values are not addressable, so it reads,
// mutates, and writes back.
func (p *params) updatePortalRange(name string, fn func(*portalRange)) {
	if p.portalRanges == nil {
		p.portalRanges = map[string]portalRange{}
	}
	pr := p.portalRanges[name]
	fn(&pr)
	p.portalRanges[name] = pr
}

// scripts returns the script-directive wire key/value pairs the params carry, in
// a stable order, with empty phases (and empty parameters) omitted. The keys are
// identical for the JSON body (Create/Update) and the URL query string (Delete),
// so both serializers draw from here.
func (p params) scripts() [][2]string {
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
	add("script", p.script)
	add("script.prerequest", p.prerequest)
	add("script.presort", p.presort)
	return out
}

// entryOptions returns the body "options" object — the data-entry modes set on a
// Create/Update — omitting each mode when unset. The result is empty (so the
// caller omits the key) when neither is set.
func (p params) entryOptions() map[string]string {
	opts := map[string]string{}
	if p.entryMode != "" {
		opts["entrymode"] = string(p.entryMode)
	}
	if p.prohibitMode != "" {
		opts["prohibitmode"] = string(p.prohibitMode)
	}
	return opts
}

// query returns the parameters that ride in the URL query string rather than a
// request body — used by the bodyless endpoints: the script directives (Delete)
// and a mod ID (UploadToContainer). The names match their body keys. Delete never
// sets a mod ID and Upload never sets scripts, so each endpoint only emits what
// applies to it.
func (p params) query() url.Values {
	v := url.Values{}
	if p.modID != "" {
		v.Set("modId", p.modID)
	}
	for _, kv := range p.scripts() {
		v.Set(kv[0], kv[1])
	}
	return v
}

// resolveCreateParams applies the create options. Deferred option errors surface
// here.
func resolveCreateParams(opts []CreateOption) (params, error) {
	var p params
	for _, opt := range opts {
		if opt != nil {
			opt.applyCreate(&p)
		}
	}
	return p, p.err
}

// resolveDuplicateParams applies the duplicate options. Deferred option errors
// surface here.
func resolveDuplicateParams(opts []DuplicateOption) (params, error) {
	var p params
	for _, opt := range opts {
		if opt != nil {
			opt.applyDuplicate(&p)
		}
	}
	return p, p.err
}

// resolveDeleteParams applies the delete options. Deferred option errors surface
// here.
func resolveDeleteParams(opts []DeleteOption) (params, error) {
	var p params
	for _, opt := range opts {
		if opt != nil {
			opt.applyDelete(&p)
		}
	}
	return p, p.err
}

// resolveFindParams applies the find options. Deferred option errors surface
// here.
func resolveFindParams(opts []FindOption) (params, error) {
	var p params
	for _, opt := range opts {
		if opt != nil {
			opt.applyFind(&p)
		}
	}
	return p, p.err
}

// resolveGetParams applies the get options. Deferred option errors surface here.
func resolveGetParams(opts []GetOption) (params, error) {
	var p params
	for _, opt := range opts {
		if opt != nil {
			opt.applyGet(&p)
		}
	}
	return p, p.err
}

// resolveGetRangeParams applies the get-range options. Deferred option errors
// surface here.
func resolveGetRangeParams(opts []GetRangeOption) (params, error) {
	var p params
	for _, opt := range opts {
		if opt != nil {
			opt.applyGetRange(&p)
		}
	}
	return p, p.err
}

// resolveConditional fills in a record-sourced mod ID for a conditional write
// (IfUnchanged). rec is nil for the *ByID forms, which cannot honor it; byIDForm
// names that form for the error hint. It also surfaces any deferred option error.
func (p *params) resolveConditional(rec *Record, byIDForm string) error {
	if p.err != nil {
		return p.err
	}
	if p.conditional && p.modID == "" {
		switch {
		case rec == nil:
			return fmt.Errorf("filemaker: IfUnchanged requires a record; use WithModID with %s", byIDForm)
		case rec.modID == "":
			return errors.New("filemaker: IfUnchanged requires a record with a ModID")
		}
		p.modID = rec.modID
	}
	return nil
}

// resolveUpdateParams applies the options and resolves the mod ID. A conditional
// update with no explicit version sources it from rec (nil for the id-addressed
// path, which cannot honor IfUnchanged). Deferred option errors surface here.
func resolveUpdateParams(opts []UpdateOption, rec *Record) (params, error) {
	var p params
	for _, opt := range opts {
		if opt != nil {
			opt.applyUpdate(&p)
		}
	}
	if err := p.resolveConditional(rec, "UpdateByID"); err != nil {
		return p, err
	}
	return p, nil
}

// resolveUploadParams applies the upload options and resolves the mod ID, the
// same way resolveUpdateParams does for writes (IfUnchanged sources it from rec).
func resolveUploadParams(opts []UploadOption, rec *Record) (params, error) {
	var p params
	for _, opt := range opts {
		if opt != nil {
			opt.applyUpload(&p)
		}
	}
	if err := p.resolveConditional(rec, "UploadToContainerByID"); err != nil {
		return p, err
	}
	return p, nil
}
