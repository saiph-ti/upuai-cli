package envparse

import (
	"strings"
	"unicode"
)

type ParsedVar struct {
	Key   string
	Value string
}

func isKeyStart(r byte) bool {
	return (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') || r == '_'
}

func isKeyChar(r byte) bool {
	return isKeyStart(r) || (r >= '0' && r <= '9')
}

func isHSpace(r byte) bool {
	return r == ' ' || r == '\t'
}

func parseDoubleQuoted(src string, i int) (string, int) {
	var sb strings.Builder
	for i < len(src) && src[i] != '"' {
		if src[i] == '\\' && i+1 < len(src) {
			switch src[i+1] {
			case 'n':
				sb.WriteByte('\n')
			case 'r':
				sb.WriteByte('\r')
			case 't':
				sb.WriteByte('\t')
			default:
				sb.WriteByte(src[i+1])
			}
			i += 2
		} else {
			sb.WriteByte(src[i])
			i++
		}
	}
	if i < len(src) && src[i] == '"' {
		i++
	}
	return sb.String(), i
}

func parseSingleQuoted(src string, i int) (string, int) {
	start := i
	for i < len(src) && src[i] != '\'' {
		i++
	}
	value := src[start:i]
	if i < len(src) && src[i] == '\'' {
		i++
	}
	return value, i
}

func parseUnquoted(src string, i int) (string, int) {
	start := i
	for i < len(src) && src[i] != '\n' {
		if isHSpace(src[i]) && i+1 < len(src) && src[i+1] == '#' {
			break
		}
		i++
	}
	return strings.TrimRightFunc(src[start:i], unicode.IsSpace), i
}

func skipToNewline(src string, i int) int {
	for i < len(src) && src[i] != '\n' {
		i++
	}
	return i
}

// Parse parses a multi-line .env-formatted text. Rules:
//   - Lines starting with `#` are comments.
//   - `export KEY=VALUE` is supported (prefix stripped).
//   - Keys must match [A-Za-z_][A-Za-z0-9_]*; lines with invalid keys are skipped.
//   - Values may be single-quoted, double-quoted, or unquoted.
//   - In unquoted values, `#` is treated as an inline comment ONLY when preceded
//     by whitespace (so `URL=https://example.com#hash` preserves the `#`).
//   - Double-quoted values support \n, \r, \t, \\, \" escapes.
//   - On duplicate keys, the last occurrence wins (silently).
func Parse(text string) []ParsedVar {
	src := strings.ReplaceAll(strings.ReplaceAll(text, "\r\n", "\n"), "\r", "\n")
	seen := map[string]int{}
	var out []ParsedVar

	i := 0
	for i < len(src) {
		for i < len(src) && (isHSpace(src[i]) || src[i] == '\n') {
			i++
		}
		if i >= len(src) {
			break
		}

		if src[i] == '#' {
			i = skipToNewline(src, i)
			continue
		}

		if i+7 <= len(src) && src[i:i+7] == "export " {
			i += 7
			for i < len(src) && isHSpace(src[i]) {
				i++
			}
		}

		if !isKeyStart(src[i]) {
			i = skipToNewline(src, i)
			continue
		}

		keyStart := i
		i++
		for i < len(src) && isKeyChar(src[i]) {
			i++
		}
		key := src[keyStart:i]

		for i < len(src) && isHSpace(src[i]) {
			i++
		}
		if i >= len(src) || src[i] != '=' {
			i = skipToNewline(src, i)
			continue
		}
		i++
		for i < len(src) && isHSpace(src[i]) {
			i++
		}

		var value string
		if i < len(src) && src[i] == '"' {
			i++
			value, i = parseDoubleQuoted(src, i)
		} else if i < len(src) && src[i] == '\'' {
			i++
			value, i = parseSingleQuoted(src, i)
		} else {
			value, i = parseUnquoted(src, i)
		}
		i = skipToNewline(src, i)

		if existing, ok := seen[key]; ok {
			out[existing] = ParsedVar{Key: key, Value: value}
		} else {
			seen[key] = len(out)
			out = append(out, ParsedVar{Key: key, Value: value})
		}
	}

	return out
}

// ParseSingle parses a single-line KEY=VALUE expression. Used by the CLI's
// `variables set` command where each argument is one assignment. Returns the
// parsed entry or `ok=false` if the input doesn't form a valid assignment.
// Same value rules as Parse (quotes, inline comment with leading whitespace).
func ParseSingle(arg string) (ParsedVar, bool) {
	parsed := Parse(arg)
	if len(parsed) == 0 {
		return ParsedVar{}, false
	}
	return parsed[0], true
}
