package filemaker

import (
	"errors"
	"fmt"
	"net/url"
)

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
func (c *recordConfig) updatePortalRange(name string, fn func(*portalRange)) {
	if c.portalRanges == nil {
		c.portalRanges = map[string]portalRange{}
	}
	pr := c.portalRanges[name]
	fn(&pr)
	c.portalRanges[name] = pr
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

// entryOptions returns the body "options" object — the data-entry modes set on a
// Create/Update — omitting each mode when unset. The result is empty (so the
// caller omits the key) when neither is set.
func (c recordConfig) entryOptions() map[string]string {
	opts := map[string]string{}
	if c.entryMode != "" {
		opts["entrymode"] = string(c.entryMode)
	}
	if c.prohibitMode != "" {
		opts["prohibitmode"] = string(c.prohibitMode)
	}
	return opts
}

// queryParams returns the parameters that ride in the URL query string rather
// than a request body — used by the bodyless endpoints: the script directives
// (Delete) and a mod ID (UploadToContainer). The names match their body keys.
// Delete never sets a mod ID and Upload never sets scripts, so each endpoint only
// emits what applies to it.
func (c recordConfig) queryParams() url.Values {
	v := url.Values{}
	if c.modID != "" {
		v.Set("modId", c.modID)
	}
	for _, kv := range c.scriptParams() {
		v.Set(kv[0], kv[1])
	}
	return v
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

// resolveDuplicateConfig applies the duplicate options. Deferred option errors
// surface here.
func resolveDuplicateConfig(opts []DuplicateOption) (recordConfig, error) {
	var cfg recordConfig
	for _, opt := range opts {
		if opt != nil {
			opt.applyDuplicate(&cfg)
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

// resolveGetConfig applies the get options. Deferred option errors surface here.
func resolveGetConfig(opts []GetOption) (recordConfig, error) {
	var cfg recordConfig
	for _, opt := range opts {
		if opt != nil {
			opt.applyGet(&cfg)
		}
	}
	return cfg, cfg.err
}

// resolveGetRangeConfig applies the get-range options. Deferred option errors
// surface here.
func resolveGetRangeConfig(opts []GetRangeOption) (recordConfig, error) {
	var cfg recordConfig
	for _, opt := range opts {
		if opt != nil {
			opt.applyGetRange(&cfg)
		}
	}
	return cfg, cfg.err
}

// resolveConditional fills in a record-sourced mod ID for a conditional write
// (IfUnchanged). rec is nil for the *ByID forms, which cannot honor it; byIDForm
// names that form for the error hint. It also surfaces any deferred option error.
func (cfg *recordConfig) resolveConditional(rec *Record, byIDForm string) error {
	if cfg.err != nil {
		return cfg.err
	}
	if cfg.conditional && cfg.modID == "" {
		switch {
		case rec == nil:
			return fmt.Errorf("filemaker: IfUnchanged requires a record; use WithModID with %s", byIDForm)
		case rec.modID == "":
			return errors.New("filemaker: IfUnchanged requires a record with a ModID")
		}
		cfg.modID = rec.modID
	}
	return nil
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
	if err := cfg.resolveConditional(rec, "UpdateByID"); err != nil {
		return cfg, err
	}
	return cfg, nil
}

// resolveUploadConfig applies the upload options and resolves the mod ID, the
// same way resolveUpdateConfig does for writes (IfUnchanged sources it from rec).
func resolveUploadConfig(opts []UploadOption, rec *Record) (recordConfig, error) {
	var cfg recordConfig
	for _, opt := range opts {
		if opt != nil {
			opt.applyUpload(&cfg)
		}
	}
	if err := cfg.resolveConditional(rec, "UploadToContainerByID"); err != nil {
		return cfg, err
	}
	return cfg, nil
}
