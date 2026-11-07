package main

import (
	"fmt"
	"os"
	"os/user"
	"runtime"
	"strings"
)

// variables holds the client-side values used for ${...} substitution in SQL
// scripts and meta commands.
type variables struct {
	hivevar  map[string]string
	hiveconf map[string]string
}

func newVariables() *variables {
	return &variables{
		hivevar:  map[string]string{},
		hiveconf: map[string]string{},
	}
}

// add parses NAME=VALUE pairs into either the hivevar or hiveconf namespace.
func (v *variables) add(pairs []string, target map[string]string) error {
	for _, pair := range pairs {
		key, value, ok := strings.Cut(pair, "=")
		key = strings.TrimSpace(key)
		if !ok || key == "" {
			return fmt.Errorf("expected NAME=VALUE, got %q", pair)
		}
		target[key] = value
	}
	return nil
}

// variableError marks a substitution failure as a usage/configuration error.
type variableError struct {
	msg string
}

func (e *variableError) Error() string { return e.msg }

func variableErrorf(format string, args ...any) error {
	return &variableError{msg: fmt.Sprintf(format, args...)}
}

// expand substitutes ${hivevar:name}, ${hiveconf:name}, ${env:NAME},
// ${system:name} and bare ${name} (a hivevar) in s. A backslash before the
// opening "${" escapes the reference. Undefined variables are an error rather
// than silently expanding to an empty string.
func (v *variables) expand(s string) (string, error) {
	if !strings.Contains(s, "${") {
		return s, nil
	}
	var b strings.Builder
	b.Grow(len(s))

	for i := 0; i < len(s); {
		if s[i] == '\\' && i+2 < len(s) && s[i+1] == '$' && s[i+2] == '{' {
			b.WriteString("${")
			i += 3
			continue
		}
		if s[i] == '$' && i+1 < len(s) && s[i+1] == '{' {
			end := strings.IndexByte(s[i+2:], '}')
			if end < 0 {
				return "", variableErrorf("unterminated ${...} reference")
			}
			name := s[i+2 : i+2+end]
			value, err := v.resolve(name)
			if err != nil {
				return "", err
			}
			b.WriteString(value)
			i += 2 + end + 1
			continue
		}
		b.WriteByte(s[i])
		i++
	}
	return b.String(), nil
}

func (v *variables) resolve(expr string) (string, error) {
	expr = strings.TrimSpace(expr)
	if expr == "" {
		return "", variableErrorf("empty ${...} reference")
	}
	prefix := ""
	name := expr
	if idx := strings.IndexByte(expr, ':'); idx >= 0 {
		prefix = strings.ToLower(expr[:idx])
		name = expr[idx+1:]
	}
	if name == "" {
		return "", variableErrorf("empty variable name in ${%s}", expr)
	}

	switch prefix {
	case "", "hivevar":
		value, ok := v.hivevar[name]
		if !ok {
			return "", variableErrorf("undefined variable %q (define it with --hivevar)", name)
		}
		return value, nil
	case "hiveconf":
		value, ok := v.hiveconf[name]
		if !ok {
			return "", variableErrorf("undefined hiveconf property %q (define it with --hiveconf)", name)
		}
		return value, nil
	case "env":
		value, ok := os.LookupEnv(name)
		if !ok {
			return "", variableErrorf("environment variable %q is not set", name)
		}
		return value, nil
	case "system":
		value, ok := systemProperty(name)
		if !ok {
			return "", variableErrorf("unknown system property %q", name)
		}
		return value, nil
	default:
		return "", variableErrorf("unknown variable namespace %q in ${%s}", prefix, expr)
	}
}

func systemProperty(name string) (string, bool) {
	switch name {
	case "user.name":
		if u, err := user.Current(); err == nil && u.Username != "" {
			return u.Username, true
		}
		if value := os.Getenv("USER"); value != "" {
			return value, true
		}
		return "", false
	case "user.home":
		if home, err := os.UserHomeDir(); err == nil && home != "" {
			return home, true
		}
		return "", false
	case "user.dir":
		if dir, err := os.Getwd(); err == nil {
			return dir, true
		}
		return "", false
	case "os.name":
		switch runtime.GOOS {
		case "linux":
			return "Linux", true
		case "darwin":
			return "Mac OS X", true
		case "windows":
			return "Windows", true
		default:
			return runtime.GOOS, true
		}
	case "os.arch":
		return runtime.GOARCH, true
	case "file.separator":
		return string(os.PathSeparator), true
	case "line.separator":
		return "\n", true
	default:
		return "", false
	}
}
