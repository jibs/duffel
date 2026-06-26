package markdown

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

func TestValidate_ValidContent(t *testing.T) {
	tests := []struct {
		name    string
		content []byte
	}{
		{"empty", []byte{}},
		{"plain text", []byte("hello world")},
		{"markdown with headers", []byte("# Title\n\n## Section\n\nParagraph")},
		{"markdown with links", []byte("[click](https://example.com)")},
		{"markdown with code block", []byte("```go\nfmt.Println()\n```")},
		{"utf8 emoji", []byte("Hello 🌍 world 🎉")},
		{"utf8 cjk", []byte("你好世界")},
		{"tabs and newlines", []byte("col1\tcol2\nrow1\trow2")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := Validate(tt.content); err != nil {
				t.Errorf("Validate(%q) = %v, want nil", tt.content, err)
			}
		})
	}
}

func TestValidate_NullBytes(t *testing.T) {
	tests := []struct {
		name    string
		content []byte
	}{
		{"null at start", []byte("\x00hello")},
		{"null in middle", []byte("hel\x00lo")},
		{"null at end", []byte("hello\x00")},
		{"only null", []byte("\x00")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := Validate(tt.content)
			if !errors.Is(err, ErrNullBytes) {
				t.Errorf("Validate(%q) = %v, want ErrNullBytes", tt.content, err)
			}
		})
	}
}

func TestValidate_BinaryContent(t *testing.T) {
	t.Run("high ratio of non-text bytes", func(t *testing.T) {
		// 100 bytes, all control chars (non-null to avoid ErrNullBytes)
		data := bytes.Repeat([]byte{0x01}, 100)
		err := Validate(data)
		if !errors.Is(err, ErrBinary) {
			t.Errorf("Validate = %v, want ErrBinary", err)
		}
	})

	t.Run("at threshold edge - just over 10%", func(t *testing.T) {
		// 100 bytes: 11 binary + 89 text = 11% → binary
		data := make([]byte, 100)
		for i := range data {
			data[i] = 'a'
		}
		for i := range 11 {
			data[i] = 0x01
		}
		err := Validate(data)
		if !errors.Is(err, ErrBinary) {
			t.Errorf("Validate = %v, want ErrBinary for 11%% non-text", err)
		}
	})

	t.Run("at threshold edge - exactly 10%", func(t *testing.T) {
		// 100 bytes: 10 binary + 90 text = exactly 10% → not > 0.1, so valid
		data := make([]byte, 100)
		for i := range data {
			data[i] = 'a'
		}
		for i := range 10 {
			data[i] = 0x01
		}
		err := Validate(data)
		if err != nil {
			t.Errorf("Validate = %v, want nil for exactly 10%% non-text", err)
		}
	})

	t.Run("just under threshold", func(t *testing.T) {
		// 100 bytes: 9 binary + 91 text = 9% → valid
		data := make([]byte, 100)
		for i := range data {
			data[i] = 'a'
		}
		for i := range 9 {
			data[i] = 0x01
		}
		if err := Validate(data); err != nil {
			t.Errorf("Validate = %v, want nil for 9%% non-text", err)
		}
	})
}

func TestValidate_LargeContent(t *testing.T) {
	t.Run("1MB valid text passes", func(t *testing.T) {
		data := []byte(strings.Repeat("Hello, world! This is valid text.\n", 32000))
		if err := Validate(data); err != nil {
			t.Errorf("Validate(1MB text) = %v, want nil", err)
		}
	})

	t.Run("1MB binary rejected", func(t *testing.T) {
		data := bytes.Repeat([]byte{0x01}, 1<<20)
		err := Validate(data)
		if !errors.Is(err, ErrBinary) {
			t.Errorf("Validate(1MB binary) = %v, want ErrBinary", err)
		}
	})
}
