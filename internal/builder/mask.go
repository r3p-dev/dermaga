// Package builder implements bare Git mirror caching, ephemeral git archive exports,
// podman build execution with resource limits, SEC-9 secret masking, and live build log streaming.
package builder

import (
	"bytes"
	"sort"
	"strings"
	"unicode/utf8"
)

const maskToken = "[MASKED]"

// MinMaskedLength is the shortest secret value that is masked. Shorter values (e.g. "b2") occur
// inside unrelated text such as commit SHAs, so masking them garbles logs without hiding anything
// worth hiding; the env editor warns that such values are not masked.
const MinMaskedLength = 4

// Masker redacts secret values from build logs and error messages (SEC-9, DEP-8).
// It handles secrets split across streaming chunk boundaries.
type Masker struct {
	secrets [][]byte
	maxLen  int
}

// NewMasker constructs a Masker from a list of secret values, ignoring values shorter than
// MinMaskedLength characters and sorting longest-first so overlapping secrets are masked completely.
func NewMasker(values []string) *Masker {
	seen := make(map[string]struct{}, len(values))
	var list [][]byte
	maxLen := 0
	for _, v := range values {
		if utf8.RuneCountInString(v) < MinMaskedLength {
			continue
		}
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{}
		b := []byte(v)
		list = append(list, b)
		if len(b) > maxLen {
			maxLen = len(b)
		}
	}
	sort.Slice(list, func(i, j int) bool {
		if len(list[i]) != len(list[j]) {
			return len(list[i]) > len(list[j])
		}
		return bytes.Compare(list[i], list[j]) < 0
	})
	return &Masker{secrets: list, maxLen: maxLen}
}

// MaskString redacts all registered secrets in a complete string.
func (m *Masker) MaskString(s string) string {
	if m == nil || len(m.secrets) == 0 || s == "" {
		return s
	}
	out := s
	for _, sec := range m.secrets {
		out = strings.ReplaceAll(out, string(sec), maskToken)
	}
	return out
}

// StreamMasker wraps an output callback and buffers up to maxLen-1 trailing bytes
// across Write calls so secrets split across chunk boundaries are still redacted.
type StreamMasker struct {
	masker *Masker
	buf    []byte
	emit   func(string)
}

// NewStreamMasker creates a chunk-safe StreamMasker forwarding masked text to emit.
func NewStreamMasker(m *Masker, emit func(string)) *StreamMasker {
	return &StreamMasker{masker: m, emit: emit}
}

// Write masks any complete secrets in buf+p and emits the safe prefix.
func (sm *StreamMasker) Write(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	if sm.masker == nil || len(sm.masker.secrets) == 0 {
		if sm.emit != nil {
			sm.emit(string(p))
		}
		return len(p), nil
	}

	sm.buf = append(sm.buf, p...)
	sm.buf = sm.maskSafePrefix(sm.buf)

	keep := sm.masker.maxLen - 1
	if keep < 0 {
		keep = 0
	}
	if len(sm.buf) > keep {
		safeEnd := len(sm.buf) - keep
		if sm.emit != nil {
			sm.emit(string(sm.buf[:safeEnd]))
		}
		sm.buf = append([]byte(nil), sm.buf[safeEnd:]...)
	}
	return len(p), nil
}

// maskSafePrefix replaces secrets only where enough lookahead bytes exist (>= maxLen from match start)
// so a shorter secret cannot mask the prefix of a longer secret split across chunks.
func (sm *StreamMasker) maskSafePrefix(buf []byte) []byte {
	maxLen := sm.masker.maxLen
	for _, sec := range sm.masker.secrets {
		searchFrom := 0
		for searchFrom <= len(buf)-len(sec) {
			idx := bytes.Index(buf[searchFrom:], sec)
			if idx < 0 {
				break
			}
			absIdx := searchFrom + idx
			if absIdx+maxLen > len(buf) {
				break
			}
			replaced := make([]byte, 0, len(buf)-len(sec)+len(maskToken))
			replaced = append(replaced, buf[:absIdx]...)
			replaced = append(replaced, maskToken...)
			replaced = append(replaced, buf[absIdx+len(sec):]...)
			buf = replaced
			searchFrom = absIdx + len(maskToken)
		}
	}
	return buf
}

// Flush masks and emits any remaining buffered bytes.
func (sm *StreamMasker) Flush() {
	if len(sm.buf) == 0 {
		return
	}
	if sm.masker != nil {
		for _, sec := range sm.masker.secrets {
			sm.buf = bytes.ReplaceAll(sm.buf, sec, []byte(maskToken))
		}
	}
	if sm.emit != nil {
		sm.emit(string(sm.buf))
	}
	sm.buf = nil
}

const maxLogLineBytes = 4096

// LineAccumulator buffers already-masked chunks into newline-delimited lines,
// flushing at maxLogLineBytes if a stream emits a long line without a newline.
type LineAccumulator struct {
	maxBytes int
	buf      strings.Builder
	onLine   func(string)
}

// NewLineAccumulator creates a LineAccumulator with the default 4 KiB line bound.
func NewLineAccumulator(onLine func(string)) *LineAccumulator {
	return &LineAccumulator{maxBytes: maxLogLineBytes, onLine: onLine}
}

// Write appends a masked chunk and emits complete lines or bounded chunks.
func (la *LineAccumulator) Write(chunk string) {
	la.buf.WriteString(chunk)
	for {
		s := la.buf.String()
		if idx := strings.IndexByte(s, '\n'); idx >= 0 {
			line := s[:idx]
			la.buf.Reset()
			la.buf.WriteString(s[idx+1:])
			if line != "" && la.onLine != nil {
				la.onLine(line)
			}
			continue
		}
		if la.maxBytes > 0 && len(s) >= la.maxBytes {
			part := s[:la.maxBytes]
			la.buf.Reset()
			la.buf.WriteString(s[la.maxBytes:])
			if la.onLine != nil {
				la.onLine(part)
			}
			continue
		}
		break
	}
}

// Flush emits any remaining buffered partial line.
func (la *LineAccumulator) Flush() {
	rem := strings.TrimRight(la.buf.String(), "\n")
	la.buf.Reset()
	if rem != "" && la.onLine != nil {
		la.onLine(rem)
	}
}
