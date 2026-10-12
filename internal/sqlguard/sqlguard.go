package sqlguard

import (
	"fmt"
	"regexp"
	"strings"
	"unicode"
)

var blacklist = []string{
	"INSERT", "UPDATE", "DELETE", "ALTER", "DROP", "TRUNCATE", "CREATE",
	"GRANT", "REVOKE", "ATTACH", "DETACH", "OPTIMIZE", "KILL", "SYSTEM",
	"RENAME", "EXCHANGE", "OUTFILE", "INFILE",
}

var ident = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

func ValidateReadOnly(query string) error {
	cleaned := stripComments(query)
	cleaned = strings.TrimSpace(cleaned)
	cleaned = strings.TrimRight(cleaned, "; \t\r\n")
	if cleaned == "" {
		return fmt.Errorf("empty query")
	}
	if strings.Contains(cleaned, ";") {
		return fmt.Errorf("only one statement is allowed")
	}
	upper := strings.ToUpper(blankStrings(cleaned))
	first := firstWord(upper)
	if first != "SELECT" && first != "WITH" {
		return fmt.Errorf("only SELECT or WITH queries are allowed")
	}
	for _, word := range blacklist {
		if containsWord(upper, word) {
			return fmt.Errorf("keyword %s is not allowed", word)
		}
	}
	if strings.Contains(upper, "SYSTEM.") || strings.Contains(upper, "INFORMATION_SCHEMA") {
		return fmt.Errorf("system tables are not allowed")
	}
	return nil
}

func CheckIdent(name string) error {
	if !ident.MatchString(name) {
		return fmt.Errorf("invalid identifier %q", name)
	}
	return nil
}

func stripComments(query string) string {
	var b strings.Builder
	inLine := false
	inBlock := false
	inString := false
	for i := 0; i < len(query); i++ {
		ch := query[i]
		if inLine {
			if ch == '\n' {
				inLine = false
				b.WriteByte('\n')
			}
			continue
		}
		if inBlock {
			if ch == '*' && i+1 < len(query) && query[i+1] == '/' {
				inBlock = false
				i++
			}
			continue
		}
		if inString {
			b.WriteByte(ch)
			if ch == '\'' {
				if i+1 < len(query) && query[i+1] == '\'' {
					b.WriteByte(query[i+1])
					i++
					continue
				}
				inString = false
			}
			continue
		}
		if ch == '-' && i+1 < len(query) && query[i+1] == '-' {
			inLine = true
			i++
			continue
		}
		if ch == '/' && i+1 < len(query) && query[i+1] == '*' {
			inBlock = true
			i++
			continue
		}
		if ch == '\'' {
			inString = true
			b.WriteByte(ch)
			continue
		}
		b.WriteByte(ch)
	}
	return b.String()
}

func blankStrings(query string) string {
	var b strings.Builder
	inString := false
	for i := 0; i < len(query); i++ {
		ch := query[i]
		if inString {
			if ch == '\'' {
				if i+1 < len(query) && query[i+1] == '\'' {
					i++
					continue
				}
				inString = false
				b.WriteByte('\'')
			}
			continue
		}
		b.WriteByte(ch)
		if ch == '\'' {
			inString = true
		}
	}
	return b.String()
}

func firstWord(query string) string {
	i := 0
	for i < len(query) && unicode.IsSpace(rune(query[i])) {
		i++
	}
	j := i
	for j < len(query) && (query[j] == '_' || (query[j] >= 'A' && query[j] <= 'Z')) {
		j++
	}
	return query[i:j]
}

func containsWord(query, word string) bool {
	for i := 0; i+len(word) <= len(query); i++ {
		if query[i:i+len(word)] != word {
			continue
		}
		beforeOK := i == 0 || !isIdent(query[i-1])
		afterOK := i+len(word) == len(query) || !isIdent(query[i+len(word)])
		if beforeOK && afterOK {
			return true
		}
	}
	return false
}

func isIdent(b byte) bool {
	return b == '_' || (b >= 'A' && b <= 'Z') || (b >= '0' && b <= '9')
}
