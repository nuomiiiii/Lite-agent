package server

import (
	"bytes"
	"unicode/utf8"

	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/encoding/unicode"
)

func decodeCommandOutput(raw []byte) string {
	raw = bytes.TrimPrefix(raw, []byte{0xEF, 0xBB, 0xBF})
	if len(raw) == 0 {
		return ""
	}
	if utf8.Valid(raw) {
		return string(raw)
	}
	if decoded, err := simplifiedchinese.GB18030.NewDecoder().Bytes(raw); err == nil && utf8.Valid(decoded) {
		return string(decoded)
	}
	if decoded, err := unicode.UTF16(unicode.LittleEndian, unicode.IgnoreBOM).NewDecoder().Bytes(raw); err == nil && utf8.Valid(decoded) {
		return string(decoded)
	}
	return string(raw)
}
