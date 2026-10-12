package sqlguard

import "testing"

func TestValidateReadOnly(t *testing.T) {
	allowed := []string{
		"SELECT name FROM rag.microservice",
		"WITH c AS (SELECT 1 AS n) SELECT n FROM c",
		"select name from rag.microservice where name = 'DROP'",
	}
	for _, query := range allowed {
		if err := ValidateReadOnly(query); err != nil {
			t.Fatalf("%s: %v", query, err)
		}
	}
	rejected := []string{
		"INSERT INTO rag.microservice VALUES (1)",
		"SELECT 1; DROP TABLE rag.microservice",
		"DELETE FROM rag.microservice",
		"SELECT * FROM system.tables",
		"UPDATE rag.microservice SET name = 'x'",
		"",
	}
	for _, query := range rejected {
		if err := ValidateReadOnly(query); err == nil {
			t.Fatalf("expected rejection for %q", query)
		}
	}
}
