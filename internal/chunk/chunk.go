package chunk

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

func Split(text string, size, overlap int) []string {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}
	if size < 1 {
		size = 800
	}
	if overlap < 0 || overlap >= size {
		overlap = 0
	}
	runes := []rune(text)
	if len(runes) <= size {
		return []string{text}
	}
	var parts []string
	for start := 0; start < len(runes); {
		end := start + size
		if end > len(runes) {
			end = len(runes)
		}
		parts = append(parts, strings.TrimSpace(string(runes[start:end])))
		if end == len(runes) {
			break
		}
		start = end - overlap
	}
	return parts
}

func TextFromUpload(filename, contentType string, body []byte) string {
	if strings.HasPrefix(contentType, "text/") || contentType == "application/json" || contentType == "" {
		if utf8.Valid(body) {
			return strings.TrimSpace(string(body))
		}
	}
	lower := strings.ToLower(filename)
	if strings.HasSuffix(lower, ".txt") || strings.HasSuffix(lower, ".md") || strings.HasSuffix(lower, ".json") {
		if utf8.Valid(body) {
			return strings.TrimSpace(string(body))
		}
	}
	return printableRuns(body)
}

func printableRuns(body []byte) string {
	var b strings.Builder
	var run []rune
	flush := func() {
		if len(run) >= 20 {
			if b.Len() > 0 {
				b.WriteByte('\n')
			}
			b.WriteString(string(run))
		}
		run = run[:0]
	}
	for len(body) > 0 {
		r, size := utf8.DecodeRune(body)
		body = body[size:]
		if r == utf8.RuneError && size == 1 {
			flush()
			continue
		}
		if unicode.IsPrint(r) || r == '\n' || r == '\t' {
			run = append(run, r)
			continue
		}
		flush()
	}
	flush()
	return strings.TrimSpace(b.String())
}
