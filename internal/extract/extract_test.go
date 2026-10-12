package extract

import "testing"

func TestParseStripsFence(t *testing.T) {
	raw := "```json\n{\"confidence\":0.91,\"rows\":[{\"name\":\"Spain\",\"code\":\"ES\"}]}\n```"
	got, err := Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	if got.Confidence != 0.91 || len(got.Rows) != 1 || got.Rows[0]["code"] != "ES" {
		t.Fatalf("%#v", got)
	}
}
