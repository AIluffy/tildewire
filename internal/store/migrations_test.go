package store

import (
	"testing"
	"testing/fstest"
)

func TestParseMigrationKeepsOnlyUpSectionAndNoTransactionFlag(t *testing.T) {
	migration, err := parseMigration("migrations/00007_example.sql", []byte(`-- +goose Up
-- +goose NO TRANSACTION
CREATE TABLE example (id INTEGER PRIMARY KEY);
-- +goose Down
DROP TABLE example;
`))
	if err != nil {
		t.Fatal(err)
	}
	if migration.version != 7 {
		t.Fatalf("version = %d, want 7", migration.version)
	}
	if !migration.noTransaction {
		t.Fatal("expected no transaction flag")
	}
	if migration.sql != "CREATE TABLE example (id INTEGER PRIMARY KEY);" {
		t.Fatalf("sql = %q", migration.sql)
	}
}

func TestLoadMigrationsSortsByNumericPrefix(t *testing.T) {
	files := fstest.MapFS{
		"migrations/00010_ten.sql": {Data: []byte("-- +goose Up\nCREATE TABLE ten (id INTEGER);\n")},
		"migrations/00002_two.sql": {Data: []byte("-- +goose Up\nCREATE TABLE two (id INTEGER);\n")},
	}
	migrations, err := loadMigrations(files, "migrations")
	if err != nil {
		t.Fatal(err)
	}
	if len(migrations) != 2 {
		t.Fatalf("migrations = %d, want 2", len(migrations))
	}
	if migrations[0].version != 2 || migrations[1].version != 10 {
		t.Fatalf("migration order = [%d %d], want [2 10]", migrations[0].version, migrations[1].version)
	}
}
