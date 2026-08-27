package config

// Layered loading, adapted from convotis-cloud/portal's internal/config rather
// than shared with it or written afresh (design G6): the interesting parts —
// pre-parse placeholder resolution (G2), schema-aware environment mapping (G5),
// the escape sentinel — are the ones a second implementation gets wrong.
//
// Sources are merged in increasing order of precedence (G1):
//
//  1. defaults declared in Go, by any nested block implementing Defaulter,
//     then the caller's Options.Defaults;
//  2. the base file, config.yaml by convention;
//  3. one overlay file per active profile (config-<profile>.yaml), in the
//     order the profiles were activated;
//  4. environment variables, ACME_*.
//
// The environment sits on top, not underneath. That is what makes this a
// migration rather than a cutover: a deployment configured entirely by ACME_*
// keeps behaving exactly as it did, because its variables still win over
// everything the files say — and it is also how a container platform expects to
// work, where the `env:` block is the last word.
//
// Every file is passed through ${VAR} / ${VAR:default} expansion before it is
// parsed (see resolve.go), so secrets and per-environment addresses can be
// injected without a file per deployment.
//
// What the portal has and this does not: the shared APP_ prefix and the
// per-service prefix layered beneath it. That machinery exists to let one
// setting apply across fourteen services; this repository ships one binary, so
// there is one prefix, ACME_ (G6).

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"time"

	"github.com/knadh/koanf/maps"
	"github.com/knadh/koanf/parsers/yaml"
	"github.com/knadh/koanf/v2"
)

// EnvPrefix is the one environment prefix this service reads. The portal
// derives its prefix per service and overlays a shared APP_ beneath it; here
// there is nothing to layer (G6).
const EnvPrefix = "ACME_"

// DefaultFile is the base config file looked up when none is configured.
const DefaultFile = "config.yaml"

// LookupFunc resolves an environment variable, mirroring os.LookupEnv.
type LookupFunc func(string) (string, bool)

// Options controls a single load. The zero value is usable: it reads
// ./config.yaml, or whatever ACME_CONFIG_FILE names, and overlays ACME_*.
type Options struct {
	// File is the base config file. When empty, ACME_CONFIG_FILE is
	// consulted, then CONFIG_FILE, then DefaultFile.
	File string

	// Profiles are activated ahead of any named by ACME_PROFILES.
	Profiles []string

	// Defaults are applied before every other source, after the defaults
	// contributed by nested Defaulter blocks. Keys are dotted paths.
	Defaults map[string]any

	// EnvAliases maps a full environment variable name onto a dotted config
	// path, for the variables whose names do not decompose into their own
	// path. ACME_SOA_PRIMARY_NS predates the grouping of G7 and lives at
	// zone.soa_primary_ns; nothing in the name says so, and G5 forbids
	// guessing. An alias is consulted before the schema, so renaming nothing
	// (G5) costs one table entry rather than a new variable.
	EnvAliases map[string]string

	// Inspect, when set, sees the fully merged tree after every layer has been
	// applied and before it is unmarshalled. It exists so a caller can report
	// a malformed value against the name the operator actually set — the
	// unmarshaller only knows the dotted path, and "cannot parse
	// 'challenge.ttl'" sends an operator looking for a file they may not have.
	// Returning an error aborts the load; the package's own Load instead
	// accumulates and repairs, which is how it keeps its "report every bad
	// setting at once" contract.
	Inspect func(*koanf.Koanf) error

	// Lookup resolves environment variables. Defaults to os.LookupEnv.
	Lookup LookupFunc

	// Environ lists the environment as "K=V" strings. Defaults to os.Environ.
	Environ []string

	// SkipValidate disables the automatic Validator pass.
	SkipValidate bool
}

// Source records where a layer of the final configuration came from. The
// service logs these at start-up: "why is this deployment behaving like that"
// is otherwise answered by guessing which overlay applied (G6).
type Source struct {
	Kind  string // "defaults", "file" or "env"
	Ref   string // file path, env prefix, or "built-in"
	Found bool   // false for an overlay file that does not exist
}

func (s Source) String() string {
	if !s.Found {
		return fmt.Sprintf("%s %s (absent)", s.Kind, s.Ref)
	}
	return fmt.Sprintf("%s %s", s.Kind, s.Ref)
}

// logSources reports the layers that applied, at info rather than debug: it is
// a handful of lines once per process, and the operator asking which overlay
// was read is usually looking at a log they cannot easily re-take with debug
// turned on (G6).
func logSources(sources []Source) {
	for _, s := range sources {
		slog.Info("configuration layer", "source", s.String())
	}
}

// loadWithSources builds a *T from the layers described above and reports which
// ones it consulted. It is the generic engine; the package's exported Load
// keeps its own signature and calls this.
func loadWithSources[T any](opts Options) (*T, []Source, error) {
	opts.normalize()

	var cfg T
	rt := reflect.TypeOf(cfg)
	if rt.Kind() != reflect.Struct {
		return nil, nil, fmt.Errorf("config: load target must be a struct, got %s", rt.Kind())
	}

	k := koanf.New(".")
	sources := []Source{{Kind: "defaults", Ref: "built-in", Found: true}}

	// 1. Defaults — block-contributed first, caller overrides after.
	defaults := map[string]any{}
	collectDefaults(rt, "", defaults)
	for key, v := range defaults {
		if err := k.Set(key, v); err != nil {
			return nil, nil, fmt.Errorf("config: default %q: %w", key, err)
		}
	}
	for key, v := range opts.Defaults {
		if err := k.Set(key, v); err != nil {
			return nil, nil, fmt.Errorf("config: default %q: %w", key, err)
		}
	}

	// 2 + 3. Base file, then one overlay per active profile.
	base, explicit := opts.baseFile()
	files := []string{base}
	for _, p := range opts.activeProfiles() {
		files = append(files, profileFile(base, p))
	}
	for i, path := range files {
		found, err := loadFile(k, path, opts.Lookup)
		if err != nil {
			return nil, nil, err
		}
		if !found && i == 0 && explicit {
			// A base file somebody configured and that is not there means the
			// deployment is wrong, and starting anyway runs on defaults nobody
			// chose. An overlay is the opposite case — it names a way this
			// deployment differs, and most deployments differ in no way at all,
			// so demanding one would make every optional overlay a required
			// stub (G4).
			//
			// The portal only fails for a path passed programmatically; a path
			// named by CONFIG_FILE slipped through. "Explicitly configured"
			// covers both here — an operator who set ACME_CONFIG_FILE has
			// stated an intention just as plainly.
			return nil, nil, fmt.Errorf("config: file %q not found", path)
		}
		sources = append(sources, Source{Kind: "file", Ref: path, Found: found})
	}

	// 4. Environment, last and therefore winning (G1).
	sc := buildSchema(rt)
	env := &envProvider{
		environ: opts.Environ,
		prefix:  EnvPrefix,
		mapKey:  sc.envMapper(EnvPrefix, opts.EnvAliases),
		isList:  sc.isList,
	}
	if err := k.Load(env, nil); err != nil {
		return nil, nil, fmt.Errorf("config: env %s*: %w", EnvPrefix, err)
	}
	sources = append(sources, Source{Kind: "env", Ref: EnvPrefix + "*", Found: true})

	if opts.Inspect != nil {
		if err := opts.Inspect(k); err != nil {
			return nil, nil, err
		}
	}

	if err := k.UnmarshalWithConf("", &cfg, koanf.UnmarshalConf{Tag: "koanf"}); err != nil {
		return nil, nil, fmt.Errorf("config: unmarshal: %w", err)
	}

	// PostLoad runs after unmarshalling but before validation, so derived
	// values are in place by the time the Validators see the config. The other
	// order makes every derived value a required one.
	if p, ok := any(&cfg).(PostLoader); ok {
		if err := p.PostLoad(); err != nil {
			return nil, nil, fmt.Errorf("config: post-load: %w", err)
		}
	}

	if !opts.SkipValidate {
		if err := validateTree(reflect.ValueOf(&cfg).Elem()); err != nil {
			return nil, nil, err
		}
	}

	return &cfg, sources, nil
}

// normalize fills in the environment the load reads. Environ and Lookup are
// two views of ONE environment, not two independent knobs: Environ is the
// overlay of ACME_* variables, Lookup resolves file placeholders and decides
// which files are read at all (ACME_CONFIG_FILE, ACME_PROFILES).
//
// Defaulting them independently is what this avoids. A caller that injected
// only Environ used to get its placeholders, its base file and its profiles
// from the developer's real environment — half an injected environment, and
// the half that chooses the files. So a supplied Environ derives the Lookup,
// and a supplied Lookup — which cannot be enumerated — leaves the overlay
// empty rather than inheriting the process's. Only a caller that supplies
// neither gets the process, which is what the service itself wants.
func (o *Options) normalize() {
	switch {
	case o.Lookup == nil && o.Environ != nil:
		o.Lookup = lookupFrom(o.Environ)
	case o.Environ == nil && o.Lookup != nil:
		o.Environ = []string{}
	}
	if o.Lookup == nil {
		o.Lookup = os.LookupEnv
	}
	if o.Environ == nil {
		o.Environ = os.Environ()
	}
}

// lookupFrom turns a "K=V" environment into a LookupFunc. Later entries win,
// as they do in a process environment.
func lookupFrom(environ []string) LookupFunc {
	m := make(map[string]string, len(environ))
	for _, kv := range environ {
		if name, value, ok := strings.Cut(kv, "="); ok {
			m[name] = value
		}
	}
	return func(name string) (string, bool) {
		v, ok := m[name]
		return v, ok
	}
}

// baseFile reports the base config path and whether anyone asked for that path
// specifically. The second half is what G4 turns on: absent-and-unconfigured is
// the environment-only deployment of G1 and is fine, absent-and-configured is a
// broken deployment.
func (o *Options) baseFile() (path string, explicit bool) {
	if o.File != "" {
		return o.File, true
	}
	if v, ok := o.Lookup(EnvPrefix + "CONFIG_FILE"); ok && v != "" {
		return v, true
	}
	if v, ok := o.Lookup("CONFIG_FILE"); ok && v != "" {
		return v, true
	}
	return DefaultFile, false
}

// activeProfiles merges explicitly passed profiles with those named by
// ACME_PROFILES, keeping activation order and dropping duplicates. The order
// matters: overlays apply in it, so the last profile named wins.
func (o *Options) activeProfiles() []string {
	raw := append([]string(nil), o.Profiles...)
	if v, ok := o.Lookup(EnvPrefix + "PROFILES"); ok {
		raw = append(raw, splitProfiles(v)...)
	}

	seen := map[string]bool{}
	out := make([]string, 0, len(raw))
	for _, p := range raw {
		p = strings.TrimSpace(p)
		if p == "" || seen[p] {
			continue
		}
		seen[p] = true
		out = append(out, p)
	}
	return out
}

func splitProfiles(v string) []string {
	return strings.FieldsFunc(v, func(r rune) bool {
		return r == ',' || r == ' ' || r == '\t'
	})
}

// profileFile turns /etc/acmeservice/config.yaml + "staging" into
// /etc/acmeservice/config-staging.yaml.
func profileFile(base, profile string) string {
	ext := filepath.Ext(base)
	return strings.TrimSuffix(base, ext) + "-" + profile + ext
}

// loadFile reads, expands and merges one YAML file. A missing file is not an
// error here; the caller decides whether that matters (G4).
//
// koanf's merge is what gives G3 for free: it descends into maps present on
// both sides and replaces every other value wholesale, lists included. A base
// of two nameservers under an overlay of one yields one, not three.
func loadFile(k *koanf.Koanf, path string, lookup LookupFunc) (bool, error) {
	raw, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("config: read %q: %w", path, err)
	}

	parsed, err := yaml.Parser().Unmarshal(resolvePlaceholders(raw, lookup))
	if err != nil {
		return false, fmt.Errorf("config: parse %q: %w", path, err)
	}

	if err := k.Load(mapProvider(pruneEmpty(parsed)), nil); err != nil {
		return false, fmt.Errorf("config: merge %q: %w", path, err)
	}
	return true, nil
}

// mapProvider hands koanf an already-parsed layer. The parse happens here
// rather than inside koanf because the tree is pruned between the two steps —
// see pruneEmpty — and koanf's own file provider gives no seam for that.
type mapProvider map[string]any

func (m mapProvider) Read() (map[string]any, error) { return m, nil }

func (m mapProvider) ReadBytes() ([]byte, error) {
	return nil, fmt.Errorf("config: mapProvider is already parsed")
}

// pruneEmpty drops keys whose value is absent — nil, or the empty string —
// from a parsed layer, so that they do not overwrite the layer beneath.
//
// This is what makes an unresolved placeholder fall through rather than erase.
// `name: "${ACME_ZONE}"` with the variable unset expands to `name: ""`, and the
// quoting is not optional (G2, task 3.2), so it is an empty STRING and not a
// null: merged as-is it replaces the base file's zone name with nothing, and
// the operator sees a value they never set and cannot find in any file. The
// overlay says "this deployment differs in these ways", and a placeholder
// nobody filled in is not one of them.
//
// It is the same rule the environment overlay already follows, for the same
// reason (see envProvider): empty means unset. The cost is symmetric too — a
// file cannot blank out a non-empty value from the layer beneath it, and has
// to state the replacement instead. That was already true of every setting
// with an ACME_* binding, since typeCheck resets an empty one to its default,
// so this makes the rule uniform rather than introducing it.
//
// Rejected: keeping the empty value and having typeCheck restore the default.
// That is what happened before, and it restores the DEFAULT — not the base
// file's value — so a base that configured something and an overlay that
// mentioned it with an unset placeholder produced neither.
func pruneEmpty(m map[string]any) map[string]any {
	out := make(map[string]any, len(m))
	for key, v := range m {
		switch val := v.(type) {
		case nil:
			continue
		case string:
			if val == "" {
				continue
			}
			out[key] = val
		case map[string]any:
			// An emptied block is dropped whole: it now says nothing, and
			// merging an empty map says nothing either.
			if child := pruneEmpty(val); len(child) > 0 {
				out[key] = child
			}
		default:
			out[key] = v
		}
	}
	return out
}

// envProvider overlays environment variables whose names the schema
// recognises. It reads from an injected environment rather than os.Environ so
// loading is testable.
type envProvider struct {
	environ []string
	prefix  string
	mapKey  func(string) string
	isList  func(string) bool
}

func (e *envProvider) ReadBytes() ([]byte, error) {
	return nil, fmt.Errorf("config: envProvider does not support ReadBytes")
}

func (e *envProvider) Read() (map[string]any, error) {
	flat := map[string]any{}
	for _, kv := range e.environ {
		name, value, ok := strings.Cut(kv, "=")
		if !ok || !strings.HasPrefix(name, e.prefix) {
			continue
		}
		key := e.mapKey(name)
		if key == "" {
			continue
		}
		// A present-but-empty variable means "unset", not "set to empty".
		// That is what the old loader did for every setting it read — it
		// tested `!ok || v == ""` on every one — and it is what compose does
		// to a variable it cannot interpolate, so the alternative silently
		// flips ACME_MIGRATE_ON_START from true to false and
		// ACME_OIDC_GROUPS_CLAIM from "groups" to nothing on a stack whose
		// .env has a blank line in it.
		//
		// The cost is a documented hole in "the environment is the last word"
		// (G1): the environment cannot BLANK OUT a value a file set, only
		// replace it, so a deployment that wants a setting empty removes it
		// from the file rather than assigning nothing to the variable. Files
		// follow the same rule for the same reason, see pruneEmpty.
		if value == "" {
			continue
		}
		if e.isList(key) {
			flat[key] = splitList(value)
			continue
		}
		flat[key] = value
	}
	return maps.Unflatten(flat, "."), nil
}

// splitList reads a comma-separated environment value as a list, because that
// is what ACME_NS_NAMES and ACME_TRUSTED_PROXY_CIDRS have always meant and G5
// renames nothing. Leaving it to the unmarshaller would not do: mapstructure's
// weak typing wraps a string in a one-element slice, so "a,b" would become the
// single nameserver "a,b" and the deployment would answer with a name nobody
// configured.
func splitList(v string) []string {
	if strings.TrimSpace(v) == "" {
		return nil
	}
	var out []string
	for _, part := range strings.Split(v, ",") {
		if part = strings.TrimSpace(part); part != "" {
			out = append(out, part)
		}
	}
	return out
}

// envMapper maps an environment variable name onto a dotted config key,
// returning "" for variables that match no key in the schema — koanf skips
// those, which keeps unrelated environment variables out of the config tree.
//
// The portal additionally accepts a legacy form in which "__" means a literal
// underscore, for environments deployed before its canonical form existed. That
// step is dropped here rather than carried along: this service has no such
// deployments, and the rule is only safe when the schema is asked first, which
// makes it a trap for whoever next edits this function.
func (s *schema) envMapper(prefix string, aliases map[string]string) func(string) string {
	return func(name string) string {
		if key, ok := aliases[name]; ok {
			return key
		}

		rest := strings.ToLower(strings.TrimPrefix(name, prefix))
		if rest == "" {
			return ""
		}

		// Canonical form: the whole name, underscores and all, matched against
		// the schema. It never guesses where to split — ACME_SOA_PRIMARY_NS
		// could split four ways and only one is a path that exists (G5).
		//
		// A block is not a match, though it is in the schema: an environment
		// value is always a scalar, so a variable landing on a whole block —
		// ACME_STAFF, ACME_LOG, anything that happens to be named after one of
		// G7's eight — can only produce `'staff' expected a map, got 'string'`
		// and abort the load. Refusing to start over a variable this service
		// does not have is the worst of the three available behaviours; the
		// alias table is where a variable that really does mean a block, as
		// ACME_ZONE means zone.name, says so.
		if key, ok := s.flat[rest]; ok && !s.blocks[key] {
			return key
		}

		// No known leaf, so this must be a key under a map-typed node, whose
		// keys the schema cannot enumerate. "__" is an explicit level
		// separator there.
		if strings.Contains(rest, "__") {
			return strings.Join(strings.Split(rest, "__"), ".")
		}

		// Without an explicit separator, match the longest known map path and
		// pass the remainder through as the map key.
		for _, mp := range s.mapPaths {
			if strings.HasPrefix(rest, mp+"_") {
				return s.mapFlat[mp] + "." + rest[len(mp)+1:]
			}
		}

		return ""
	}
}

// Defaulter is implemented by config blocks that carry their own defaults.
// The loader walks the target struct and merges the map returned by every
// nested block that implements it, keyed relative to that block's position.
type Defaulter interface {
	Defaults() map[string]any
}

// Validator is implemented by config blocks that can check themselves. The
// loader calls Validate on every nested block that implements it, innermost
// first, and finally on the top-level struct.
type Validator interface {
	Validate() error
}

// PostLoader is implemented by a top-level config that derives values from what
// was loaded. It runs after unmarshalling and before validation.
type PostLoader interface {
	PostLoad() error
}

var durationType = reflect.TypeOf(time.Duration(0))

// schema is the flattened view of a config struct: every leaf key it can hold,
// which of those leaves are lists, plus the subtrees that are maps (whose keys
// are open-ended).
type schema struct {
	// flat maps an underscore-joined key ("oidc_client_id") to the dotted
	// koanf path ("oidc.client_id").
	flat map[string]string
	// lists holds the dotted paths of slice-typed leaves, whose environment
	// values are comma-separated.
	lists map[string]bool
	// blocks holds the dotted paths of nested struct nodes. They are
	// addressable keys like any other, but never from the environment: see
	// envMapper.
	blocks map[string]bool
	// mapPaths holds the underscore-joined paths of map-typed nodes, longest
	// first, so env lookups can match a prefix and pass the rest through.
	mapPaths []string
	// mapFlat resolves a mapPaths entry back to its dotted path.
	mapFlat map[string]string
}

// buildSchema derives the key schema of T by reflection over `koanf` tags.
// Deriving it from the struct — rather than from whatever a YAML file happens
// to contain — is what lets environment variables map back onto keys whose own
// names contain underscores (ACME_OIDC_GROUPS_CLAIM → oidc.groups_claim).
func buildSchema(t reflect.Type) *schema {
	s := &schema{
		flat:    map[string]string{},
		lists:   map[string]bool{},
		blocks:  map[string]bool{},
		mapFlat: map[string]string{},
	}
	s.walk(t, "")

	for p := range s.mapFlat {
		s.mapPaths = append(s.mapPaths, p)
	}
	sort.Slice(s.mapPaths, func(i, j int) bool { return len(s.mapPaths[i]) > len(s.mapPaths[j]) })

	return s
}

func (s *schema) isList(path string) bool { return s.lists[path] }

func (s *schema) walk(t reflect.Type, prefix string) {
	for t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	if t.Kind() != reflect.Struct {
		return
	}

	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if !f.IsExported() {
			continue
		}

		name, squash := fieldKey(f)
		if name == "-" {
			continue
		}

		path := name
		if squash {
			path = strings.TrimSuffix(prefix, ".")
		} else if prefix != "" {
			path = prefix + "." + name
		}

		ft := f.Type
		for ft.Kind() == reflect.Ptr {
			ft = ft.Elem()
		}

		switch {
		case ft == durationType:
			s.addLeaf(path)
		case ft.Kind() == reflect.Struct && !isOpaqueStruct(ft):
			if squash {
				s.walk(ft, prefix)
			} else {
				s.addLeaf(path) // the block itself is addressable too
				s.blocks[path] = true
				s.walk(ft, path)
			}
		case ft.Kind() == reflect.Map:
			s.addLeaf(path)
			s.mapFlat[flatten(path)] = path
		case ft.Kind() == reflect.Slice && ft.Elem().Kind() != reflect.Uint8:
			s.addLeaf(path)
			s.lists[path] = true
		default:
			s.addLeaf(path)
		}
	}
}

func (s *schema) addLeaf(path string) {
	if path == "" {
		return
	}
	f := flatten(path)
	// Deterministic on collision: the first path in lexical order wins, so the
	// mapping does not depend on the order reflect happens to report fields in.
	if existing, ok := s.flat[f]; !ok || path < existing {
		s.flat[f] = path
	}
}

// fieldKey returns the koanf key for a struct field and whether the field is
// squashed into its parent.
func fieldKey(f reflect.StructField) (name string, squash bool) {
	tag := f.Tag.Get("koanf")
	parts := strings.Split(tag, ",")
	for _, opt := range parts[1:] {
		if opt == "squash" {
			squash = true
		}
	}
	name = parts[0]
	if name == "" {
		if squash || f.Anonymous {
			return "", true
		}
		name = strings.ToLower(f.Name)
	}
	return name, squash
}

// isOpaqueStruct reports whether a struct type should be treated as a leaf
// value rather than a nested block. time.Time and empty structs must not be
// descended into, or the schema explodes into leaves nobody can address.
func isOpaqueStruct(t reflect.Type) bool {
	return t.PkgPath() == "time" || t.NumField() == 0
}

func flatten(path string) string { return strings.ReplaceAll(path, ".", "_") }

// collectDefaults walks T and merges the Defaults() of every nested block that
// implements Defaulter, prefixed with that block's path.
func collectDefaults(t reflect.Type, prefix string, out map[string]any) {
	for t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	if t.Kind() != reflect.Struct {
		return
	}

	// Instantiated through reflect.New, so Defaults() must be declared on the
	// value receiver to be found here.
	if d, ok := reflect.New(t).Interface().(Defaulter); ok {
		for k, v := range d.Defaults() {
			key := k
			if prefix != "" {
				key = prefix + "." + k
			}
			out[key] = v
		}
	}

	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if !f.IsExported() {
			continue
		}
		name, squash := fieldKey(f)
		if name == "-" {
			continue
		}

		ft := f.Type
		for ft.Kind() == reflect.Ptr {
			ft = ft.Elem()
		}
		if ft.Kind() != reflect.Struct || isOpaqueStruct(ft) {
			continue
		}

		child := name
		if squash {
			child = prefix
		} else if prefix != "" {
			child = prefix + "." + name
		}
		collectDefaults(ft, child, out)
	}
}

// validateTree calls Validate on every nested block that implements Validator,
// innermost first, then on v itself. Only addressable values are considered, so
// a Validate declared on the pointer receiver is still found.
func validateTree(v reflect.Value) error {
	if v.Kind() == reflect.Ptr {
		if v.IsNil() {
			return nil
		}
		v = v.Elem()
	}

	if v.Kind() == reflect.Struct && !isOpaqueStruct(v.Type()) {
		for i := 0; i < v.NumField(); i++ {
			f := v.Type().Field(i)
			if !f.IsExported() {
				continue
			}
			if name, _ := fieldKey(f); name == "-" {
				continue
			}
			if err := validateTree(v.Field(i)); err != nil {
				return err
			}
		}
	}

	if v.CanAddr() {
		if val, ok := v.Addr().Interface().(Validator); ok {
			return val.Validate()
		}
	}
	return nil
}
