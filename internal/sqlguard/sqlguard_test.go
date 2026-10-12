package sqlguard

import "testing"

func TestValidateReadOnly(t *testing.T) {
	allowed := []string{
		"SELECT name FROM rag.country",
		"WITH c AS (SELECT 1 AS n) SELECT n FROM c",
		"select code from rag.country where name = 'DROP'",
	}
	for _, query := range allowed {
		if err := ValidateReadOnly(query); err != nil {
			t.Fatalf("%s: %v", query, err)
		}
	}
	rejected := []string{
		"INSERT INTO rag.country VALUES (1)",
		"SELECT 1; DROP TABLE rag.country",
		"DELETE FROM rag.country",
		"SELECT * FROM system.tables",
		"UPDATE rag.country SET name = 'x'",
		"",
	}
	for _, query := range rejected {
		if err := ValidateReadOnly(query); err == nil {
			t.Fatalf("expected rejection for %q", query)
		}
	}
}
