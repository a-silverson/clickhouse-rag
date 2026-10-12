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
	for _, name := range []string{"rag.files", "rag.chunks", "rag.extractions", "rag.country", "rag.developer", "rag.location", "rag.project", "rag.apartments"} {
		if !strings.Contains(joined, name) {
			t.Fatalf("missing %s", name)
		}
	}
	if strings.Contains(strings.ToUpper(joined), "ALTER TABLE") && strings.Contains(strings.ToUpper(joined), " UPDATE ") {
		t.Fatal("migration must not use ALTER TABLE UPDATE")
	}
	if !strings.Contains(joined, "checksum") {
		t.Fatal("missing file checksum column")
	}
	if strings.Contains(joined, "Nullable(") {
		t.Fatal("migration should use defaults instead of Nullable")
	}
}
