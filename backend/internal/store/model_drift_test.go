package store

import (
	"strings"
	"testing"

	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/model"
	"gorm.io/gorm"
)

// TestIntegrationModelDrift checks the GORM structs against the migrated schema:
// every model's table exists, every mapped field has a column of a compatible type,
// and every column is mapped by some field. It catches the common mistake — adding
// a field and forgetting the migration — at test time rather than at first query.
//
// AutoMigrate is deliberately not called here or anywhere else (design D8); the
// schema under test is the one the migrations produced.
func TestIntegrationModelDrift(t *testing.T) {
	db, _ := migratedDB(t)
	mig := db.Migrator()

	for _, m := range model.Models() {
		stmt := &gorm.Statement{DB: db}
		if err := stmt.Parse(m); err != nil {
			t.Fatalf("parse model %T: %v", m, err)
		}
		table := stmt.Schema.Table

		if !mig.HasTable(m) {
			t.Errorf("model %T maps table %q, which does not exist — is a migration missing?", m, table)
			continue
		}

		columnTypes, err := mig.ColumnTypes(m)
		if err != nil {
			t.Fatalf("column types for %q: %v", table, err)
		}
		dbTypes := map[string]string{}
		for _, ct := range columnTypes {
			dbTypes[ct.Name()] = ct.DatabaseTypeName()
		}

		for _, name := range stmt.Schema.DBNames {
			dbType, ok := dbTypes[name]
			if !ok {
				t.Errorf("model %T maps field %q to column %s.%s, which does not exist — "+
					"add a migration for it", m, stmt.Schema.FieldsByDBName[name].Name, table, name)
				continue
			}
			field := stmt.Schema.FieldsByDBName[name]
			modelType := string(field.DataType)
			if got, want := typeClass(dbType), typeClass(modelType); got != want {
				t.Errorf("column %s.%s is %s (%s) but field %s maps as %s (%s)",
					table, name, dbType, got, field.Name, modelType, want)
			}
			delete(dbTypes, name)
		}
		for name := range dbTypes {
			t.Errorf("column %s.%s exists in the schema but no field of %T maps it", table, name, m)
		}
	}
}

// typeClass reduces a PostgreSQL type name or a GORM data type to a coarse class,
// which is all a drift check needs: it is looking for "field has no column" and
// "text field over an integer column", not for exact type equality.
func typeClass(s string) string {
	switch strings.ToLower(strings.TrimSuffix(s, " with time zone")) {
	case "text", "string", "varchar", "character varying", "char", "bpchar", "uuid", "inet", "cidr", "macaddr":
		return "text"
	case "int", "int2", "int4", "int8", "uint", "smallint", "integer", "bigint", "serial", "bigserial":
		return "int"
	case "float", "float4", "float8", "real", "double precision", "numeric", "decimal":
		return "float"
	case "bool", "boolean":
		return "bool"
	case "time", "timestamp", "timestamptz", "date", "datetime":
		return "time"
	case "bytes", "bytea", "json", "jsonb":
		return "bytes"
	case "text[]", "_text", "varchar[]", "_varchar", "array":
		return "array"
	default:
		return strings.ToLower(s)
	}
}
