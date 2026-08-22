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

	"github.com/jackc/pgx/v5/pgtype"
)

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
		&Project{}, &LoadBalancer{}, &LBNode{}, &Alias{}, &Domain{},
		&APIKey{}, &APIKeyNetwork{}, &Challenge{}, &Session{}, &AuditEntry{},
		&ChallengeInteraction{}, &ProjectDailyStat{},
	}
}

// ProjectDailyStat is one project's inventory size on one day: the only thing in
// the reporting capability that is stored rather than aggregated on read, because
// inventory rows carry no deletion record and yesterday's size cannot be
// reconstructed from today's tables. See the migration for the full reasoning.
//
// The row is a level, not an increment, which is what makes writing it twice in a
// day harmless — see reporting.Snapshot.
type ProjectDailyStat struct {
	ProjectID         int64     `gorm:"primaryKey;column:project_id"`
	Day               time.Time `gorm:"primaryKey;column:day;type:date"`
	ProjectSlug       string    `gorm:"column:project_slug"`
	AliasCount        int       `gorm:"column:alias_count"`
	DomainCount       int       `gorm:"column:domain_count"`
	LoadBalancerCount int       `gorm:"column:load_balancer_count"`
}

func (ProjectDailyStat) TableName() string { return "project_daily_stats" }

type Project struct {
	ID        int64 `gorm:"primaryKey"`
	Slug      string
	Name      string
	CreatedAt time.Time

	LoadBalancers []LoadBalancer `gorm:"foreignKey:ProjectID"`
}

func (Project) TableName() string { return "projects" }

type LoadBalancer struct {
	ID        int64 `gorm:"primaryKey"`
	ProjectID int64 `gorm:"column:project_id"`
	Slug      string
	Name      string
	CreatedAt time.Time

	Project *Project `gorm:"foreignKey:ProjectID"`
	Nodes   []LBNode `gorm:"foreignKey:LoadBalancerID"`
	Domains []Domain `gorm:"foreignKey:LoadBalancerID"`
}

func (LoadBalancer) TableName() string { return "load_balancers" }

type LBNode struct {
	ID             int64   `gorm:"primaryKey"`
	LoadBalancerID int64   `gorm:"column:load_balancer_id"`
	Label          string  // 'a' / 'b'
	IPv4           *string `gorm:"column:ipv4;type:inet"`
	IPv6           *string `gorm:"column:ipv6;type:inet"`
}

func (LBNode) TableName() string { return "lb_nodes" }

// Alias is one challenge name, owned by exactly one load balancer. Several domains
// (typically example.com and *.example.com) share one alias.
type Alias struct {
	ID             int64  `gorm:"primaryKey"`
	LoadBalancerID int64  `gorm:"column:load_balancer_id"`
	ChallengeFQDN  string `gorm:"column:challenge_fqdn"`
	Label          string
	CreatedAt      time.Time

	LoadBalancer *LoadBalancer `gorm:"foreignKey:LoadBalancerID"`
	Domains      []Domain      `gorm:"foreignKey:AliasID"`
}

func (Alias) TableName() string { return "aliases" }

type Domain struct {
	ID             int64 `gorm:"primaryKey"`
	LoadBalancerID int64 `gorm:"column:load_balancer_id"`
	AliasID        int64 `gorm:"column:alias_id"`
	Name           string
	CreatedAt      time.Time

	Alias *Alias `gorm:"foreignKey:AliasID"`
}

func (Domain) TableName() string { return "domains" }

// APIKey stores only the SHA-256 digest of the secret; the secret itself is shown
// once at creation and never persisted (design D3).
type APIKey struct {
	ID             int64  `gorm:"primaryKey"`
	LoadBalancerID int64  `gorm:"column:load_balancer_id"`
	Identifier     string // 'k_7f3a91c4', non-secret
	SecretHash     []byte `gorm:"column:secret_hash"`
	DisplayPrefix  string
	Note           string
	CreatedAt      time.Time
	ExpiresAt      *time.Time
	LastUsedAt     *time.Time
	DisabledAt     *time.Time

	LoadBalancer *LoadBalancer   `gorm:"foreignKey:LoadBalancerID"`
	Networks     []APIKeyNetwork `gorm:"foreignKey:APIKeyID"`
}

func (APIKey) TableName() string { return "api_keys" }

// APIKeyNetwork is one CIDR block a key is pinned to. No rows means unrestricted.
type APIKeyNetwork struct {
	ID       int64  `gorm:"primaryKey"`
	APIKeyID int64  `gorm:"column:api_key_id"`
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
	ID             int64       `gorm:"primaryKey"`
	ProjectID      *int64      `gorm:"column:project_id"`
	LoadBalancerID *int64      `gorm:"column:load_balancer_id"`
	AliasID        *int64      `gorm:"column:alias_id"`
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
	ID          int64 `gorm:"primaryKey"`
	AliasID     int64 `gorm:"column:alias_id"`
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
	ID           int64 `gorm:"primaryKey"`
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
	ObjectID     *int64
	ObjectLabel  string
	ProjectID    *int64
	Before       json.RawMessage `gorm:"type:jsonb"`
	After        json.RawMessage `gorm:"type:jsonb"`
}

func (AuditEntry) TableName() string { return "audit_entries" }
