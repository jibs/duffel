package markdown

import (
	"bytes"
	"errors"
)

var (
	ErrNullBytes = errors.New("content contains null bytes")
	ErrBinary    = errors.New("content appears to be binary")
)

// Validate checks that content is valid text suitable for markdown storage.
func Validate(content []byte) error {
	if bytes.ContainsRune(content, '\x00') {
		return ErrNullBytes
	}
	// Simple binary detection: high ratio of non-text bytes
	if len(content) > 0 {
		nonText := 0
		for _, b := range content {
			if b < 0x09 || (b > 0x0d && b < 0x20 && b != 0x1b) {
				nonText++
			}
		}
		if float64(nonText)/float64(len(content)) > 0.1 {
			return ErrBinary
		}
	}
	return nil
}
