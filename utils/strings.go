package utils

import (
	"strings"
	"unicode"
)

// SanitizeFileToken 将任意字符串压缩为适合文件名的 token（仅保留字母数字与 .-_，其余替换为 _）
func SanitizeFileToken(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return "unknown"
	}
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '.' || r == '-' || r == '_' {
			b.WriteRune(r)
		} else {
			b.WriteByte('_')
		}
	}
	out := b.String()
	out = strings.Trim(out, "._-")
	if out == "" {
		return "unknown"
	}
	return out
}


