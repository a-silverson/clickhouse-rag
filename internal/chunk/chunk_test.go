package chunk

import "testing"

func TestSplitOverlap(t *testing.T) {
	parts := Split("abcdefghij", 4, 1)
	if len(parts) < 3 {
		t.Fatalf("%#v", parts)
	}
	if parts[0] != "abcd" || parts[1] != "defg" {
		t.Fatalf("%#v", parts)
	}
}

func TestTextFromPDFBytes(t *testing.T) {
	body := []byte("%PDF-1.4\n(Hello from a brochure about the coast)\n")
	got := TextFromUpload("brochure.pdf", "application/pdf", body)
	if got == "" {
		t.Fatal("expected printable text")
	}
}
