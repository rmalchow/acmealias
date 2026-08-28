package store

import (
	"fmt"
	"os"
	"sort"
	"strings"
	"testing"

	"gorm.io/gorm"
)

const goldenPath = "schema.sql"

// TestIntegrationSchemaGolden applies every migration to a fresh schema, dumps the
// result and compares it against the checked-in golden file. A migration that
// changes the schema without updating the golden file fails here.
//
// This is a local guard — CI does not set TEST_DATABASE_URL and skips it (design
// D13). The compensating control is the acceptance run.
func TestIntegrationSchemaGolden(t *testing.T) {
	db, _ := scopedDB(t)

	got := dumpSchema(t, db)

	if os.Getenv("UPDATE_GOLDEN") != "" {
		if err := os.WriteFile(goldenPath, []byte(got), 0o644); err != nil {
			t.Fatalf("write %s: %v", goldenPath, err)
		}
		t.Logf("wrote %s", goldenPath)
		return
	}

	want, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("read %s: %v\nregenerate it with: "+
			"cd backend && TEST_DATABASE_URL=... UPDATE_GOLDEN=1 go test ./internal/store -run SchemaGolden",
			goldenPath, err)
	}
	if string(want) != got {
		t.Errorf("migrated schema does not match %s.\n%s\n\n"+
			"If the migration is intended, regenerate the golden file with:\n"+
			"  cd backend && TEST_DATABASE_URL=... UPDATE_GOLDEN=1 go test ./internal/store -run SchemaGolden\n"+
			"and review the diff in the commit.",
			goldenPath, firstDiff(string(want), got))
	}
}

// dumpSchema renders the current schema as deterministic text, read from
// information_schema and pg_indexes. No pg_dump binary is involved, so the test
// needs no PostgreSQL client in the image.
func dumpSchema(t *testing.T, db *gorm.DB) string {
	t.Helper()

	var schema string
	if err := db.Raw("select current_schema()").Scan(&schema).Error; err != nil {
		t.Fatalf("current_schema: %v", err)
	}

	var b strings.Builder

	type column struct {
		TableName, ColumnName, DataType, IsNullable, ColumnDefault string
	}
	var columns []column
	err := db.Raw(`
		select table_name, column_name, data_type, is_nullable,
		       coalesce(column_default, '') as column_default
		  from information_schema.columns
		 where table_schema = ? and table_name <> 'schema_migrations'
		 order by table_name, column_name`, schema).Scan(&columns).Error
	if err != nil {
		t.Fatalf("read columns: %v", err)
	}

	type constraint struct {
		TableName, ConstraintType, ConstraintName, Columns string
	}
	var constraints []constraint
	err = db.Raw(`
		select tc.table_name, tc.constraint_type, tc.constraint_name,
		       coalesce(string_agg(kcu.column_name, ',' order by kcu.ordinal_position), '') as columns
		  from information_schema.table_constraints tc
		  left join information_schema.key_column_usage kcu
		    on kcu.constraint_name = tc.constraint_name
		   and kcu.constraint_schema = tc.constraint_schema
		 where tc.table_schema = ? and tc.table_name <> 'schema_migrations'
		   -- PostgreSQL 16+ materializes a system CHECK per NOT NULL column, named
		   -- after OIDs that differ per database. Nullability is in the columns above.
		   and tc.constraint_name not like '%\_not\_null'
		 group by tc.table_name, tc.constraint_type, tc.constraint_name
		 order by tc.table_name, tc.constraint_type, tc.constraint_name`, schema).Scan(&constraints).Error
	if err != nil {
		t.Fatalf("read constraints: %v", err)
	}

	// CHECK EXPRESSIONS, not just the constraint's name. information_schema's
	// table_constraints says a check exists; it does not say what it checks, so
	// `check (kind = 'team' or credits = 0)` and `check (true)` are the same row
	// there. An invariant expressed as a constraint is an invariant this file
	// must be able to see change.
	type checkExpr struct{ TableName, ConstraintName, Expr string }
	var checks []checkExpr
	err = db.Raw(`
		select rel.relname as table_name, con.conname as constraint_name,
		       pg_get_constraintdef(con.oid) as expr
		  from pg_constraint con
		  join pg_class rel on rel.oid = con.conrelid
		  join pg_namespace ns on ns.oid = rel.relnamespace
		 where ns.nspname = ? and con.contype = 'c'
		   and rel.relname <> 'schema_migrations'
		 order by rel.relname, con.conname`, schema).Scan(&checks).Error
	if err != nil {
		t.Fatalf("read check expressions: %v", err)
	}

	// ROW-LEVEL SECURITY, per table. This is tenant isolation, and until now the
	// golden file could not see it at all: a migration that dropped a policy, or
	// forgot FORCE so the owning role bypassed it, produced a byte-identical dump.
	// Four cross-account bugs have already hidden behind RLS returning no rows and
	// no error; a schema guard that cannot see the policies is not guarding the
	// thing most worth guarding.
	type rls struct {
		TableName           string
		RowSecurity, Forced bool
	}
	var rlsRows []rls
	err = db.Raw(`
		select rel.relname as table_name, rel.relrowsecurity as row_security,
		       rel.relforcerowsecurity as forced
		  from pg_class rel
		  join pg_namespace ns on ns.oid = rel.relnamespace
		 where ns.nspname = ? and rel.relkind = 'r'
		   and rel.relname <> 'schema_migrations'
		 order by rel.relname`, schema).Scan(&rlsRows).Error
	if err != nil {
		t.Fatalf("read row security: %v", err)
	}

	type policy struct{ Tablename, Policyname, Cmd, Qual, Withcheck string }
	var policies []policy
	err = db.Raw(`
		select tablename, policyname, cmd,
		       coalesce(qual, '') as qual, coalesce(with_check, '') as withcheck
		  from pg_policies
		 where schemaname = ?
		 order by tablename, policyname`, schema).Scan(&policies).Error
	if err != nil {
		t.Fatalf("read policies: %v", err)
	}

	// TRIGGERS, and the source of the functions they call. A trigger is how an
	// invariant is enforced when it cannot be a constraint — accounts.kind being
	// immutable, for one — and it is invisible in information_schema.columns.
	type trigger struct{ TableName, TriggerName, Def string }
	var triggers []trigger
	err = db.Raw(`
		select rel.relname as table_name, tg.tgname as trigger_name,
		       pg_get_triggerdef(tg.oid) as def
		  from pg_trigger tg
		  join pg_class rel on rel.oid = tg.tgrelid
		  join pg_namespace ns on ns.oid = rel.relnamespace
		 where ns.nspname = ? and not tg.tgisinternal
		 order by rel.relname, tg.tgname`, schema).Scan(&triggers).Error
	if err != nil {
		t.Fatalf("read triggers: %v", err)
	}

	type function struct{ Name, Def string }
	var functions []function
	err = db.Raw(`
		select p.proname as name, pg_get_functiondef(p.oid) as def
		  from pg_proc p
		  join pg_namespace ns on ns.oid = p.pronamespace
		 where ns.nspname = ?
		 order by p.proname`, schema).Scan(&functions).Error
	if err != nil {
		t.Fatalf("read functions: %v", err)
	}

	type index struct{ Tablename, Indexname, Indexdef string }
	var indexes []index
	err = db.Raw(`
		select tablename, indexname, indexdef
		  from pg_indexes
		 where schemaname = ? and tablename <> 'schema_migrations'
		 order by tablename, indexname`, schema).Scan(&indexes).Error
	if err != nil {
		t.Fatalf("read indexes: %v", err)
	}

	// The schema is named per test run, so strip it from anything that embeds it.
	clean := func(s string) string {
		s = strings.ReplaceAll(s, schema+".", "")
		return strings.ReplaceAll(s, " "+schema+" ", " ")
	}

	byTable := map[string][]string{}
	tables := map[string]bool{}
	for _, c := range columns {
		tables[c.TableName] = true
		line := fmt.Sprintf("  column %s %s %s", c.ColumnName, c.DataType,
			map[string]string{"YES": "null", "NO": "not null"}[c.IsNullable])
		if c.ColumnDefault != "" {
			line += " default " + clean(c.ColumnDefault)
		}
		byTable[c.TableName] = append(byTable[c.TableName], line)
	}
	for _, c := range constraints {
		tables[c.TableName] = true
		byTable[c.TableName] = append(byTable[c.TableName],
			fmt.Sprintf("  constraint %s %s (%s)", c.ConstraintName, strings.ToLower(c.ConstraintType), c.Columns))
	}
	for _, i := range indexes {
		tables[i.Tablename] = true
		byTable[i.Tablename] = append(byTable[i.Tablename],
			fmt.Sprintf("  index %s %s", i.Indexname, clean(i.Indexdef)))
	}
	for _, c := range checks {
		tables[c.TableName] = true
		byTable[c.TableName] = append(byTable[c.TableName],
			fmt.Sprintf("  check %s %s", c.ConstraintName, clean(c.Expr)))
	}
	for _, r := range rlsRows {
		tables[r.TableName] = true
		byTable[r.TableName] = append(byTable[r.TableName],
			fmt.Sprintf("  rls enabled=%t forced=%t", r.RowSecurity, r.Forced))
	}
	for _, p := range policies {
		tables[p.Tablename] = true
		line := fmt.Sprintf("  policy %s for %s using (%s)", p.Policyname, strings.ToLower(p.Cmd), clean(p.Qual))
		if p.Withcheck != "" {
			line += fmt.Sprintf(" with check (%s)", clean(p.Withcheck))
		}
		byTable[p.Tablename] = append(byTable[p.Tablename], line)
	}
	for _, tg := range triggers {
		tables[tg.TableName] = true
		byTable[tg.TableName] = append(byTable[tg.TableName],
			fmt.Sprintf("  trigger %s %s", tg.TriggerName, clean(tg.Def)))
	}

	names := make([]string, 0, len(tables))
	for name := range tables {
		names = append(names, name)
	}
	sort.Strings(names)

	b.WriteString("-- Generated by TestIntegrationSchemaGolden; do not edit by hand.\n" +
		"-- Regenerate: TEST_DATABASE_URL=... UPDATE_GOLDEN=1 go test ./internal/store -run SchemaGolden\n")
	for _, name := range names {
		fmt.Fprintf(&b, "\ntable %s\n", name)
		lines := byTable[name]
		sort.Strings(lines)
		for _, l := range lines {
			b.WriteString(l)
			b.WriteString("\n")
		}
	}

	// Functions last, and not under a table: a trigger function belongs to the
	// invariant it enforces rather than to the table that happens to fire it, and
	// pg_get_functiondef gives the whole body — so a rule changed inside one is a
	// diff here rather than an invisible edit.
	for _, f := range functions {
		fmt.Fprintf(&b, "\nfunction %s\n", f.Name)
		for _, l := range strings.Split(strings.TrimSpace(clean(f.Def)), "\n") {
			b.WriteString("  " + strings.TrimRight(l, " ") + "\n")
		}
	}
	return b.String()
}

func firstDiff(want, got string) string {
	w, g := strings.Split(want, "\n"), strings.Split(got, "\n")
	for i := 0; i < len(w) || i < len(g); i++ {
		lw, lg := "", ""
		if i < len(w) {
			lw = w[i]
		}
		if i < len(g) {
			lg = g[i]
		}
		if lw != lg {
			return fmt.Sprintf("first difference at line %d:\n  golden: %q\n  actual: %q", i+1, lw, lg)
		}
	}
	return "files differ but no differing line was found"
}
