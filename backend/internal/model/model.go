// Package model holds the GORM structs that map onto the schema created by the
// migrations in internal/store/migrations.
//
// The schema is owned by golang-migrate, never by GORM: AutoMigrate is not called
// anywhere in this tree (design D8). These structs are a query layer only. Keeping
// them in step with the migrations is the job of the two tests in internal/store —
// schema_golden_test.go and model_drift_test.go.
//
// ponytail: PostgreSQL inet/cidr columns are mapped as strings rather than as
// netip.Addr/netip.Prefix, which would need Scanner/Valuer wrappers. Callers parse
// with netip. Ceiling: no compile-time guarantee the string is a valid address;
// upgrade path is a small named type with Scan/Value if that ever bites.
package model

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"gorm.io/gorm"
)

// ID is the identifier type for every row in this schema (design P17). It is a
// UUIDv7: a 48-bit millisecond timestamp followed by randomness, so rows sort
// roughly by creation and index inserts stay at the right-hand edge, while the
// value discloses neither how many of a thing exist nor what the next one will be.
//
// NewID is the only way application code should mint one. The database has a
// uuidv7() default for rows created by SQL, but Go-side generation keeps the
// identifier known before the insert, which several callers rely on.
type ID = uuid.UUID

// NilID is the zero identifier. It is never a valid stored value.
var NilID ID

// NewID returns a fresh UUIDv7.
func NewID() ID {
	id, err := uuid.NewV7()
	if err != nil {
		// uuid.NewV7 fails only if crypto/rand fails, which is not a condition a
		// caller can do anything useful with.
		panic(fmt.Sprintf("model: generate identifier: %v", err))
	}
	return id
}

// ParseID parses the canonical text form, as it arrives in a URL path or a
// request body.
func ParseID(s string) (ID, error) { return uuid.Parse(s) }

// ensureID fills in an identifier that the caller left unset.
//
// This exists because of how GORM decides whether to send a column. A field it
// can recognize as empty — a zero int, a nil pointer — is omitted so the
// database default applies. An ID is a [16]byte array, which GORM sends as-is,
// so an unset identifier arrives as the all-zero UUID and the SECOND insert
// collides on the primary key. The failure is loud but its cause is not, so the
// remedy lives here rather than in every caller.
//
// The database still carries a uuidv7() default, for rows created by SQL that
// never passes through these structs.
func ensureID(id *ID) {
	if *id == NilID {
		*id = NewID()
	}
}

// BeforeCreate hooks mint an identifier for any row inserted without one. GORM
// calls them for every create path, including the raw ones tests use.
func (a *Account) BeforeCreate(*gorm.DB) error              { ensureID(&a.ID); return nil }
func (p *Project) BeforeCreate(*gorm.DB) error              { ensureID(&p.ID); return nil }
func (lb *LoadBalancer) BeforeCreate(*gorm.DB) error        { ensureID(&lb.ID); return nil }
func (n *LBNode) BeforeCreate(*gorm.DB) error               { ensureID(&n.ID); return nil }
func (a *Alias) BeforeCreate(*gorm.DB) error                { ensureID(&a.ID); return nil }
func (d *Domain) BeforeCreate(*gorm.DB) error               { ensureID(&d.ID); return nil }
func (k *APIKey) BeforeCreate(*gorm.DB) error               { ensureID(&k.ID); return nil }
func (n *APIKeyNetwork) BeforeCreate(*gorm.DB) error        { ensureID(&n.ID); return nil }
func (c *Challenge) BeforeCreate(*gorm.DB) error            { ensureID(&c.ID); return nil }
func (e *AuditEntry) BeforeCreate(*gorm.DB) error           { ensureID(&e.ID); return nil }
func (i *ChallengeInteraction) BeforeCreate(*gorm.DB) error { ensureID(&i.ID); return nil }
func (u *User) BeforeCreate(*gorm.DB) error                 { ensureID(&u.ID); return nil }
func (r *Role) BeforeCreate(*gorm.DB) error                 { ensureID(&r.ID); return nil }
func (m *AccountMember) BeforeCreate(*gorm.DB) error        { ensureID(&m.ID); return nil }
func (g *RoleGrant) BeforeCreate(*gorm.DB) error            { ensureID(&g.ID); return nil }
func (g *APIKeyGrant) BeforeCreate(*gorm.DB) error          { ensureID(&g.ID); return nil }

// StringArray maps a PostgreSQL text[] column onto []string.
//
// ponytail: the GORM driver is pgx-backed but goes through database/sql, which has
// no array support of its own, so the two directions are delegated to pgx's own
// type map rather than to a second Postgres library.
type StringArray []string

var pgTypes = pgtype.NewMap()

func (a *StringArray) Scan(src any) error {
	if src == nil {
		*a = nil
		return nil
	}
	return pgTypes.SQLScanner((*[]string)(a)).Scan(src)
}

func (a StringArray) Value() (driver.Value, error) {
	// A nil slice encodes as an EMPTY array, not SQL NULL. Every text[] column in
	// this schema is `not null default '{}'`, so returning nil here would make an
	// unset field a NOT NULL violation at insert time — a 500 for the caller and a
	// bug that only shows up for the one code path that forgot to set the field.
	if a == nil {
		a = StringArray{}
	}
	buf, err := pgTypes.Encode(pgtype.TextArrayOID, pgtype.TextFormatCode, []string(a), nil)
	if err != nil {
		return nil, fmt.Errorf("encode text[]: %w", err)
	}
	return string(buf), nil
}

// Models lists every struct mapped onto a table, in dependency order. The drift
// test walks it, so a new model must be added here.
func Models() []any {
	return []any{
		&Account{}, &User{}, &Role{}, &AccountMember{}, &RoleGrant{},
		&Project{}, &LoadBalancer{}, &LBNode{}, &Alias{}, &Domain{},
		&APIKey{}, &APIKeyGrant{}, &APIKeyNetwork{}, &Challenge{}, &Session{},
		&AuditEntry{}, &ChallengeInteraction{}, &ProjectDailyStat{},
	}
}

// Account owns inventory and anchors every grant. The internal edition has exactly
// one, seeded by the migration; the public edition has one per customer.
//
// Nothing below an account may name it as the object of a grant: operations on the
// account itself are governed by membership, not by grants (design P14).
type Account struct {
	ID        ID `gorm:"primaryKey"`
	Slug      string
	Name      string
	CreatedAt time.Time
}

func (Account) TableName() string { return "accounts" }

// User is a person, globally (design P18). They exist before belonging to any
// account, may belong to several, and outlive the deletion of any of them —
// which is why this table has no account_id and no policy. What confines a
// listing of people is account_members, which has both.
//
// Subject is the identity provider's stable identifier for them, not their email
// address: an address changes and is reused, a subject does not.
type User struct {
	ID          ID `gorm:"primaryKey"`
	Subject     string
	Email       *string
	DisplayName string `gorm:"column:display_name"`
	CreatedAt   time.Time
}

func (User) TableName() string { return "users" }

// Role is a named set of grants inside one account. Roles are rows rather than
// four constants in Go, which is the whole point of the change: what a customer
// needs their operators to be able to do is not a thing this codebase can
// enumerate in advance.
type Role struct {
	ID          ID `gorm:"primaryKey"`
	AccountID   ID `gorm:"column:account_id"`
	Name        string
	Description string
	// Unrestricted marks the account's fixed role: every action on every object
	// of that account. It is a flag rather than a set of grants so that it
	// cannot be edited away one row at a time, and so "there is always a way
	// back into an account" holds without depending on which rows survive.
	Unrestricted bool
	CreatedAt    time.Time

	Grants []RoleGrant `gorm:"foreignKey:RoleID"`
}

func (Role) TableName() string { return "roles" }

// AccountMember binds a user to a role in an account. A person holds roles
// through membership; roles held in one account confer nothing in another.
type AccountMember struct {
	ID        ID `gorm:"primaryKey"`
	AccountID ID `gorm:"column:account_id"`
	UserID    ID `gorm:"column:user_id"`
	RoleID    ID `gorm:"column:role_id"`
	CreatedAt time.Time

	User *User `gorm:"foreignKey:UserID"`
	Role *Role `gorm:"foreignKey:RoleID"`
}

func (AccountMember) TableName() string { return "account_members" }

// RoleGrant is one grant held by a role. ObjectID and Selector are mutually
// exclusive and both null for a wildcard identifier; the database enforces the
// same rule, so a row that disagrees cannot be stored.
type RoleGrant struct {
	ID          ID              `gorm:"primaryKey"`
	RoleID      ID              `gorm:"column:role_id"`
	AccountID   ID              `gorm:"column:account_id"`
	ObjectType  string          `gorm:"column:object_type"`
	ObjectID    *ID             `gorm:"column:object_id"`
	Selector    json.RawMessage `gorm:"column:selector;type:jsonb"`
	Action      string
	Inheritable bool
	CreatedAt   time.Time
}

func (RoleGrant) TableName() string { return "role_grants" }

// APIKeyGrant is one grant held by an API key. Same shape as RoleGrant, in its
// own table: the two subjects have different lifecycles, different write paths
// and different audit vocabulary, and one table would need a check constraint
// saying exactly one owner is set — the same complexity somewhere less obvious.
type APIKeyGrant struct {
	ID          ID              `gorm:"primaryKey"`
	APIKeyID    ID              `gorm:"column:api_key_id"`
	AccountID   ID              `gorm:"column:account_id"`
	ObjectType  string          `gorm:"column:object_type"`
	ObjectID    *ID             `gorm:"column:object_id"`
	Selector    json.RawMessage `gorm:"column:selector;type:jsonb"`
	Action      string
	Inheritable bool
	CreatedAt   time.Time
}

func (APIKeyGrant) TableName() string { return "api_key_grants" }

// ProjectDailyStat is one project's inventory size on one day: the only thing in
// the reporting capability that is stored rather than aggregated on read, because
// inventory rows carry no deletion record and yesterday's size cannot be
// reconstructed from today's tables. See the migration for the full reasoning.
//
// The row is a level, not an increment, which is what makes writing it twice in a
// day harmless — see reporting.Snapshot.
type ProjectDailyStat struct {
	ProjectID         ID        `gorm:"primaryKey;column:project_id"`
	Day               time.Time `gorm:"primaryKey;column:day;type:date"`
	ProjectSlug       string    `gorm:"column:project_slug"`
	AliasCount        int       `gorm:"column:alias_count"`
	DomainCount       int       `gorm:"column:domain_count"`
	LoadBalancerCount int       `gorm:"column:load_balancer_count"`
}

func (ProjectDailyStat) TableName() string { return "project_daily_stats" }

type Project struct {
	ID        ID `gorm:"primaryKey"`
	AccountID ID `gorm:"column:account_id"`
	Slug      string
	Name      string
	CreatedAt time.Time

	LoadBalancers []LoadBalancer `gorm:"foreignKey:ProjectID"`
}

func (Project) TableName() string { return "projects" }

type LoadBalancer struct {
	ID        ID `gorm:"primaryKey"`
	AccountID ID `gorm:"column:account_id"`
	ProjectID ID `gorm:"column:project_id"`
	Slug      string
	Name      string
	CreatedAt time.Time

	Project *Project `gorm:"foreignKey:ProjectID"`
	Nodes   []LBNode `gorm:"foreignKey:LoadBalancerID"`
	Domains []Domain `gorm:"foreignKey:LoadBalancerID"`
}

func (LoadBalancer) TableName() string { return "load_balancers" }

type LBNode struct {
	ID             ID      `gorm:"primaryKey"`
	AccountID      ID      `gorm:"column:account_id"`
	ProjectID      ID      `gorm:"column:project_id"`
	LoadBalancerID ID      `gorm:"column:load_balancer_id"`
	Label          string  // 'a' / 'b'
	IPv4           *string `gorm:"column:ipv4;type:inet"`
	IPv6           *string `gorm:"column:ipv6;type:inet"`
}

func (LBNode) TableName() string { return "lb_nodes" }

// Alias is one challenge name, owned by exactly one load balancer. Several domains
// (typically example.com and *.example.com) share one alias.
type Alias struct {
	ID             ID     `gorm:"primaryKey"`
	AccountID      ID     `gorm:"column:account_id"`
	ProjectID      ID     `gorm:"column:project_id"`
	LoadBalancerID ID     `gorm:"column:load_balancer_id"`
	ChallengeFQDN  string `gorm:"column:challenge_fqdn"`
	Label          string
	CreatedAt      time.Time

	LoadBalancer *LoadBalancer `gorm:"foreignKey:LoadBalancerID"`
	Domains      []Domain      `gorm:"foreignKey:AliasID"`
}

func (Alias) TableName() string { return "aliases" }

type Domain struct {
	ID             ID `gorm:"primaryKey"`
	AccountID      ID `gorm:"column:account_id"`
	ProjectID      ID `gorm:"column:project_id"`
	LoadBalancerID ID `gorm:"column:load_balancer_id"`
	AliasID        ID `gorm:"column:alias_id"`
	Name           string
	CreatedAt      time.Time

	Alias *Alias `gorm:"foreignKey:AliasID"`
}

func (Domain) TableName() string { return "domains" }

// APIKey stores only the SHA-256 digest of the secret; the secret itself is shown
// once at creation and never persisted (design D3).
type APIKey struct {
	ID        ID `gorm:"primaryKey"`
	AccountID ID `gorm:"column:account_id"`
	// There is no LoadBalancerID. A key is a subject anchored to an account,
	// and what it may reach is its grants — see migration 000009 for why the
	// column could not stay alongside them.
	Identifier    string // 'k_7f3a91c4', non-secret
	SecretHash    []byte `gorm:"column:secret_hash"`
	DisplayPrefix string
	Note          string
	CreatedAt     time.Time
	ExpiresAt     *time.Time
	LastUsedAt    *time.Time
	DisabledAt    *time.Time

	Networks []APIKeyNetwork `gorm:"foreignKey:APIKeyID"`
	// Grants are preloaded with the key on the authentication path, so the
	// challenge handler adds no query to check a permission (constraint 2).
	Grants []APIKeyGrant `gorm:"foreignKey:APIKeyID"`
}

func (APIKey) TableName() string { return "api_keys" }

// APIKeyNetwork is one CIDR block a key is pinned to. No rows means unrestricted.
type APIKeyNetwork struct {
	ID       ID     `gorm:"primaryKey"`
	APIKeyID ID     `gorm:"column:api_key_id"`
	CIDR     string `gorm:"column:cidr;type:cidr"`
}

func (APIKeyNetwork) TableName() string { return "api_key_networks" }

// ChallengeInteraction is one publication seen through to its end: who published
// it and from where, whether the CA ever queried it, and when it was withdrawn.
//
// Deliberately free of foreign keys to aliases and domains, and carrying their
// labels as text — a history that disappears when a domain is deleted cannot
// answer the question it exists for. See the migration for the full reasoning.
type ChallengeInteraction struct {
	ID             ID          `gorm:"primaryKey"`
	ProjectID      *ID         `gorm:"column:project_id"`
	LoadBalancerID *ID         `gorm:"column:load_balancer_id"`
	AliasID        *ID         `gorm:"column:alias_id"`
	AliasLabel     string      `gorm:"column:alias_label"`
	ChallengeFQDN  string      `gorm:"column:challenge_fqdn"`
	DomainNames    StringArray `gorm:"column:domain_names;type:text[]"`
	KeyID          *string     `gorm:"column:key_id"`
	ClientIP       *string     `gorm:"column:client_ip;type:inet"`
	FQDNForm       string      `gorm:"column:fqdn_form"`
	ValueDigest    string      `gorm:"column:value_digest"`
	PublishedAt    time.Time   `gorm:"column:published_at"`
	WithdrawnAt    *time.Time  `gorm:"column:withdrawn_at"`
	Abandoned      bool        `gorm:"column:abandoned"`
	FirstQueriedAt *time.Time  `gorm:"column:first_queried_at"`
	LastQueriedAt  *time.Time  `gorm:"column:last_queried_at"`
	QueryCount     int64       `gorm:"column:query_count"`
}

func (ChallengeInteraction) TableName() string { return "challenge_interactions" }

// Challenge is one TXT value published at an alias. PublishedBy holds the key
// identifier as text, deliberately not a foreign key (design D6).
type Challenge struct {
	ID          ID `gorm:"primaryKey"`
	AliasID     ID `gorm:"column:alias_id"`
	Value       string
	PublishedBy *string
	CreatedAt   time.Time
	ExpiresAt   time.Time

	Alias *Alias `gorm:"foreignKey:AliasID"`
}

func (Challenge) TableName() string { return "challenges" }

// Session is server-side console state referenced by an opaque cookie value. It
// holds no provider tokens; Groups is the claim as seen at sign-in.
type Session struct {
	ID          string `gorm:"primaryKey"`
	Subject     string
	DisplayName string
	Groups      StringArray `gorm:"type:text[]"`
	CreatedAt   time.Time
	LastSeenAt  time.Time
	ExpiresAt   time.Time
	RevokedAt   *time.Time
}

func (Session) TableName() string { return "sessions" }

// AuditEntry is append-only. ObjectLabel and ProjectID are denormalized on purpose
// so an entry stays readable after the object it describes is deleted.
type AuditEntry struct {
	ID           ID  `gorm:"primaryKey"`
	AccountID    *ID `gorm:"column:account_id"`
	OccurredAt   time.Time
	ActorSubject *string
	ActorName    *string
	ActorGroups  StringArray `gorm:"type:text[]"`
	ActorRoles   StringArray `gorm:"type:text[]"`
	ActorSource  string      // 'console' | 'cli' | 'system'
	RequestID    *string
	ClientIP     *string `gorm:"column:client_ip;type:inet"`
	Action       string
	ObjectType   string
	ObjectID     *ID
	ObjectLabel  string
	ProjectID    *ID
	Before       json.RawMessage `gorm:"type:jsonb"`
	After        json.RawMessage `gorm:"type:jsonb"`
}

func (AuditEntry) TableName() string { return "audit_entries" }
