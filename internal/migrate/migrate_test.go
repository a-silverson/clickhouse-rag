package migrate

import (
	"strings"
	"testing"
)

func TestStatementsCoverTables(t *testing.T) {
	statements, err := Statements()
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(statements, "\n")
	for _, name := range []string{"rag.files", "rag.chunks", "rag.extractions", "rag.microservice", "rag.api", "rag.pulsar_topic"} {
		if !strings.Contains(joined, name) {
			t.Fatalf("missing %s", name)
		}
	}
	if strings.Contains(joined, "rag.country") || strings.Contains(joined, "rag.apartments") {
		t.Fatal("real-estate tables must not remain in the migration")
	}
	if strings.Contains(strings.ToUpper(joined), "ALTER TABLE") {
		t.Fatal("migration must be a single create script, not ALTER TABLE")
	}
	if !strings.Contains(joined, "checksum") {
		t.Fatal("missing file checksum column")
	}
	if strings.Contains(joined, "Nullable(") {
		t.Fatal("migration should use defaults instead of Nullable")
	}
}
