// Package sqlsplit splits a SQL script into statements on a configurable
// terminator that is outside string literals, quoted identifiers and comments.
package sqlsplit

import "strings"

// Split returns the statements in script, using ";" as the terminator.
func Split(script string) []string {
	return SplitWith(script, ";")
}

// SplitWith returns the statements in script. A trailing statement without a
// terminator is returned as-is. Statements consisting only of whitespace and
// comments are dropped. An empty delimiter is treated as ";".
func SplitWith(script, delimiter string) []string {
	return scan(script, delimiter).statements
}

// Complete reports whether script ends with a statement terminator (";")
// outside strings and comments.
func Complete(script string) bool {
	return CompleteWith(script, ";")
}

// CompleteWith reports whether script ends with delimiter outside strings and
// comments.
func CompleteWith(script, delimiter string) bool {
	if strings.TrimSpace(script) == "" {
		return false
	}
	return scan(script, delimiter).complete
}

type scanResult struct {
	statements []string
	complete   bool
}

func scan(script, delimiter string) scanResult {
	if delimiter == "" {
		delimiter = ";"
	}

	const (
		normal = iota
		singleQuote
		doubleQuote
		backtick
		lineComment
		blockComment
	)

	var res scanResult
	var buf strings.Builder
	state := normal
	escaped := false
	lastDelimiter := false

	flush := func() {
		stmt := strings.TrimSpace(buf.String())
		buf.Reset()
		if stmt != "" && hasCode(stmt) {
			res.statements = append(res.statements, stmt)
		}
	}

	for i := 0; i < len(script); i++ {
		c := script[i]
		switch state {
		case normal:
			if strings.HasPrefix(script[i:], delimiter) {
				flush()
				lastDelimiter = true
				i += len(delimiter) - 1
				continue
			}
			switch c {
			case '\'':
				state = singleQuote
				lastDelimiter = false
				buf.WriteByte(c)
			case '"':
				state = doubleQuote
				lastDelimiter = false
				buf.WriteByte(c)
			case '`':
				state = backtick
				lastDelimiter = false
				buf.WriteByte(c)
			case '-':
				if i+1 < len(script) && script[i+1] == '-' {
					state = lineComment
					buf.WriteString("--")
					i++
				} else {
					lastDelimiter = false
					buf.WriteByte(c)
				}
			case '/':
				if i+1 < len(script) && script[i+1] == '*' {
					state = blockComment
					buf.WriteString("/*")
					i++
				} else {
					lastDelimiter = false
					buf.WriteByte(c)
				}
			default:
				if !isSpace(c) {
					lastDelimiter = false
				}
				buf.WriteByte(c)
			}

		case singleQuote, doubleQuote, backtick:
			buf.WriteByte(c)
			if escaped {
				escaped = false
				continue
			}
			switch c {
			case '\\':
				escaped = true
			case '\'':
				if state == singleQuote {
					// SQL escapes a quote by doubling it.
					if i+1 < len(script) && script[i+1] == '\'' {
						buf.WriteByte('\'')
						i++
					} else {
						state = normal
					}
				}
			case '"':
				if state == doubleQuote {
					if i+1 < len(script) && script[i+1] == '"' {
						buf.WriteByte('"')
						i++
					} else {
						state = normal
					}
				}
			case '`':
				if state == backtick {
					if i+1 < len(script) && script[i+1] == '`' {
						buf.WriteByte('`')
						i++
					} else {
						state = normal
					}
				}
			}

		case lineComment:
			buf.WriteByte(c)
			if c == '\n' {
				state = normal
			}

		case blockComment:
			buf.WriteByte(c)
			if c == '*' && i+1 < len(script) && script[i+1] == '/' {
				buf.WriteByte('/')
				i++
				state = normal
			}
		}
	}

	flush()
	res.complete = (state == normal || state == lineComment) && lastDelimiter
	return res
}

func isSpace(c byte) bool {
	return c == ' ' || c == '\t' || c == '\r' || c == '\n'
}

// hasCode reports whether s contains anything other than whitespace and
// comments.
func hasCode(s string) bool {
	const (
		normal = iota
		lineComment
		blockComment
		singleQuote
		doubleQuote
		backtick
	)
	state := normal
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch state {
		case normal:
			switch c {
			case '-':
				if i+1 < len(s) && s[i+1] == '-' {
					state = lineComment
					i++
				} else {
					return true
				}
			case '/':
				if i+1 < len(s) && s[i+1] == '*' {
					state = blockComment
					i++
				} else {
					return true
				}
			case '\'':
				state = singleQuote
			case '"':
				state = doubleQuote
			case '`':
				state = backtick
			default:
				if !isSpace(c) {
					return true
				}
			}
		case lineComment:
			if c == '\n' {
				state = normal
			}
		case blockComment:
			if c == '*' && i+1 < len(s) && s[i+1] == '/' {
				i++
				state = normal
			}
		case singleQuote, doubleQuote, backtick:
			// A string literal by itself is not executable SQL, but it only
			// occurs after a keyword in valid statements; treat it as code.
			return true
		}
	}
	return false
}
