// Package sqlsplit splits a SQL script into statements on semicolons that are
// outside string literals, quoted identifiers and comments.
package sqlsplit

import "strings"

// Split returns the statements in script. A trailing statement without a
// semicolon is returned as-is. Statements consisting only of whitespace and
// comments are dropped.
func Split(script string) []string {
	var out []string
	var buf strings.Builder

	const (
		normal = iota
		singleQuote
		doubleQuote
		backtick
		lineComment
		blockComment
	)
	state := normal
	escaped := false

	flush := func() {
		stmt := strings.TrimSpace(buf.String())
		buf.Reset()
		if stmt != "" && hasCode(stmt) {
			out = append(out, stmt)
		}
	}

	for i := 0; i < len(script); i++ {
		c := script[i]
		switch state {
		case normal:
			switch c {
			case '\'':
				state = singleQuote
				buf.WriteByte(c)
			case '"':
				state = doubleQuote
				buf.WriteByte(c)
			case '`':
				state = backtick
				buf.WriteByte(c)
			case '-':
				if i+1 < len(script) && script[i+1] == '-' {
					state = lineComment
					buf.WriteString("--")
					i++
				} else {
					buf.WriteByte(c)
				}
			case '/':
				if i+1 < len(script) && script[i+1] == '*' {
					state = blockComment
					buf.WriteString("/*")
					i++
				} else {
					buf.WriteByte(c)
				}
			case ';':
				flush()
			default:
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

	stmt := strings.TrimSpace(buf.String())
	if stmt != "" && hasCode(stmt) {
		out = append(out, stmt)
	}
	return out
}

// Complete reports whether script ends with a statement terminator outside
// strings and comments.
func Complete(script string) bool {
	if strings.TrimSpace(script) == "" {
		return false
	}
	const (
		normal = iota
		singleQuote
		doubleQuote
		backtick
		lineComment
		blockComment
	)
	state := normal
	escaped := false
	var last byte
	for i := 0; i < len(script); i++ {
		c := script[i]
		switch state {
		case normal:
			switch c {
			case '\'':
				state = singleQuote
				last = c
			case '"':
				state = doubleQuote
				last = c
			case '`':
				state = backtick
				last = c
			case '-':
				if i+1 < len(script) && script[i+1] == '-' {
					state = lineComment
					i++
				} else {
					last = c
				}
			case '/':
				if i+1 < len(script) && script[i+1] == '*' {
					state = blockComment
					i++
				} else {
					last = c
				}
			case ';':
				last = c
			default:
				if !isSpace(c) {
					last = c
				}
			}
		case singleQuote, doubleQuote, backtick:
			if escaped {
				escaped = false
				last = c
				continue
			}
			switch c {
			case '\\':
				escaped = true
				last = c
			case '\'':
				if state == singleQuote {
					if i+1 < len(script) && script[i+1] == '\'' {
						i++
						last = c
					} else {
						state = normal
						last = c
					}
				} else {
					last = c
				}
			case '"':
				if state == doubleQuote {
					if i+1 < len(script) && script[i+1] == '"' {
						i++
						last = c
					} else {
						state = normal
						last = c
					}
				} else {
					last = c
				}
			case '`':
				if state == backtick {
					if i+1 < len(script) && script[i+1] == '`' {
						i++
						last = c
					} else {
						state = normal
						last = c
					}
				} else {
					last = c
				}
			default:
				last = c
			}
		case lineComment:
			if c == '\n' {
				state = normal
			}
		case blockComment:
			if c == '*' && i+1 < len(script) && script[i+1] == '/' {
				i++
				state = normal
			}
		}
	}
	switch state {
	case normal, lineComment:
		return last == ';'
	default:
		return false
	}
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
