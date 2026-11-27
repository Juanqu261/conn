// Package lineend compares installer-managed text files across line-ending
// conversion. Conn writes LF files, and Git with core.autocrlf=true, the Git
// for Windows default, checks committed files out with CRLF line endings. An
// installer that compared bytes would then report its own unchanged file as
// modified and refuse to update or remove it.
package lineend

import "bytes"

var (
	crlf = []byte("\r\n")
	lf   = []byte("\n")
)

// Normalize returns content with every CRLF replaced by LF.
func Normalize(content []byte) []byte {
	if !bytes.Contains(content, crlf) {
		return content
	}
	return bytes.ReplaceAll(content, crlf, lf)
}

// Equal reports whether a and b differ at most in CRLF versus LF line
// endings.
func Equal(a, b []byte) bool {
	return bytes.Equal(Normalize(a), Normalize(b))
}

// UsesCRLF reports whether content has CRLF line endings, so text inserted
// into it can follow the same convention.
func UsesCRLF(content []byte) bool {
	return bytes.Contains(content, crlf)
}

// Match converts LF text to CRLF when like uses CRLF.
func Match(text, like []byte) []byte {
	if !UsesCRLF(like) {
		return text
	}
	return bytes.ReplaceAll(Normalize(text), lf, crlf)
}
