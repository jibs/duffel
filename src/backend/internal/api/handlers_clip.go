package api

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"regexp"
	"strings"
	"time"
	"unicode"

	"duffel/src/backend/internal/storage"
)

// dataURIRe matches markdown image tags with embedded data URIs.
// Full pattern: ![alttext](data:image/TYPE;base64,DATA)
// Capture groups: (1) mime subtype, (2) base64 data.
var dataURIRe = regexp.MustCompile(`!\[[^\]]*\]\(data:image/(png|jpe?g|gif|webp|svg\+xml);base64,([A-Za-z0-9+/=]+)\)`)

type clipRequest struct {
	NoteName    string `json:"noteName"`
	Frontmatter string `json:"frontmatter"`
	Content     string `json:"content"`
	Path        string `json:"path"`
	URL         string `json:"url"`
}

func handleClip(store *storage.Store, onContentChanged func()) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, 10*1024*1024)

		var req clipRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid JSON body", "/api/clip")
			return
		}

		if req.NoteName == "" {
			writeError(w, http.StatusBadRequest, "noteName is required", "/api/clip")
			return
		}
		if req.Content == "" {
			writeError(w, http.StatusBadRequest, "content is required", "/api/clip")
			return
		}

		// Normalize and validate destination directory.
		dir := req.Path
		if dir == "" {
			dir = "clips/"
		} else if !strings.HasSuffix(dir, "/") {
			dir += "/"
		}
		if err := validateClipPath(dir); err != nil {
			writeError(w, http.StatusBadRequest, err.Error(), "/api/clip")
			return
		}

		slug := slugify(req.NoteName)

		// If the client already embedded a frontmatter block at the top of content
		// (e.g. some clients send the full document in content AND a separate
		// frontmatter field), strip the embedded one so we don't duplicate it.
		body := req.Content
		if req.Frontmatter != "" {
			body = stripLeadingFrontmatter(body)
		}

		// Extract and save embedded images, replacing data URIs with relative paths.
		content := extractAndSaveImages(w, store, body, dir, slug)
		if content == "" && body != "" {
			// extractAndSaveImages already wrote the error response.
			return
		}

		// Assemble file content.  Sanitize the frontmatter block before joining
		// so that rogue "---" sequences inside YAML values (injected via page
		// content) cannot prematurely close the frontmatter block.
		var assembled string
		if req.Frontmatter != "" {
			assembled = sanitizeFrontmatter(req.Frontmatter) + "\n\n" + content
		} else {
			assembled = content
		}

		// Generate filename with date suffix.
		filename := slug + "-" + time.Now().Format("2006-01-02") + ".md"
		finalPath := dir + filename

		// Collision avoidance: append -2 through -99 until the path is free.
		if _, err := store.StatFile(finalPath); err == nil {
			found := false
			for i := 2; i <= 99; i++ {
				candidate := dir + slug + "-" + time.Now().Format("2006-01-02") + fmt.Sprintf("-%d", i) + ".md"
				if _, err := store.StatFile(candidate); errors.Is(err, storage.ErrNotFound) {
					finalPath = candidate
					found = true
					break
				}
			}
			if !found {
				writeError(w, http.StatusConflict, "could not find a unique filename", finalPath)
				return
			}
		}

		if err := store.Write(finalPath, []byte(assembled)); err != nil {
			writeFSWriteError(w, err, finalPath)
			return
		}

		triggerContentChanged(onContentChanged)

		writeJSON(w, http.StatusCreated, map[string]string{"path": finalPath})
	}
}

// extractAndSaveImages finds all embedded data URIs in content, writes each as
// a binary attachment file, and returns the content with those URIs replaced by
// relative paths.
//
// Errors are handled per-image:
//   - A base64 decode failure leaves the original data URI in place (the note
//     is still saved; the bad image simply stays embedded as a data URI).
//   - A filesystem write failure is fatal: the HTTP error is written and the
//     function returns "" so the caller aborts.
func extractAndSaveImages(w http.ResponseWriter, store *storage.Store, content, dir, slug string) string {
	counter := 0
	var writeErr error

	result := dataURIRe.ReplaceAllStringFunc(content, func(match string) string {
		if writeErr != nil {
			return match
		}

		sub := dataURIRe.FindStringSubmatch(match)
		if sub == nil {
			return match
		}
		mimeSubtype := sub[1] // e.g. "png", "jpeg", "svg+xml"
		b64Data := sub[2]

		ext := mimeSubtypeToExt(mimeSubtype)
		counter++
		filename := fmt.Sprintf("%s-%d.%s", slug, counter, ext)
		attachPath := dir + "attachments/" + filename

		decoded, err := base64.StdEncoding.DecodeString(b64Data)
		if err != nil {
			// Non-fatal: keep the original data URI and log a warning.
			log.Printf("clip: skipping image %s: base64 decode: %v", filename, err)
			counter-- // don't consume the counter slot for a skipped image
			return match
		}

		if err := store.WriteRaw(attachPath, decoded); err != nil {
			writeErr = err
			return match
		}

		return fmt.Sprintf("![](attachments/%s)", filename)
	})

	if writeErr != nil {
		writeFSWriteError(w, writeErr, dir+"attachments/")
		return ""
	}
	return result
}

// mimeSubtypeToExt converts an image MIME subtype to a file extension.
func mimeSubtypeToExt(subtype string) string {
	switch strings.ToLower(subtype) {
	case "jpeg":
		return "jpg"
	case "svg+xml":
		return "svg"
	default:
		return strings.ToLower(subtype)
	}
}

// sanitizeFrontmatter removes any bare "---" separator lines that appear inside
// YAML value strings.  These would prematurely close the YAML front-matter
// block when the content is embedded into a Markdown file, potentially allowing
// an attacker to inject arbitrary Markdown by crafting a page title or other
// property value.
//
// The function preserves the opening and closing "---" delimiters of the block
// itself; only interior occurrences (between the first and last separator line)
// are replaced with a safe placeholder.
func sanitizeFrontmatter(fm string) string {
	// Split on newlines, find the opening and closing "---" lines, then scrub
	// any additional "---" lines that appear between them.
	lines := strings.Split(fm, "\n")
	if len(lines) < 2 {
		return fm
	}

	// Locate the opening delimiter (first line that is "---").
	openIdx := -1
	for i, l := range lines {
		if strings.TrimRight(l, "\r") == "---" {
			openIdx = i
			break
		}
	}
	if openIdx < 0 {
		return fm
	}

	// Locate the closing delimiter (last line that is "---").
	closeIdx := -1
	for i := len(lines) - 1; i > openIdx; i-- {
		if strings.TrimRight(lines[i], "\r") == "---" {
			closeIdx = i
			break
		}
	}
	if closeIdx < 0 {
		return fm
	}

	// Scrub any interior "---" lines (between open and close) by replacing
	// them with a harmless placeholder.
	for i := openIdx + 1; i < closeIdx; i++ {
		if strings.TrimRight(lines[i], "\r") == "---" {
			lines[i] = "# (sanitized)"
		}
	}

	return strings.Join(lines, "\n")
}

// stripLeadingFrontmatter removes a leading YAML frontmatter block (delimited
// by "---" lines) from s if one is present.  It is used to deduplicate
// frontmatter when a client sends the same block in both req.Frontmatter and
// embedded at the top of req.Content.
func stripLeadingFrontmatter(s string) string {
	if !strings.HasPrefix(s, "---\n") {
		return s
	}
	rest := s[4:] // skip opening "---\n"
	idx := strings.Index(rest, "\n---\n")
	if idx >= 0 {
		return strings.TrimPrefix(rest[idx+5:], "\n") // skip closing "\n---\n" + optional blank line
	}
	// Handle "---" at very end of string (no trailing newline after closer)
	if after, ok := strings.CutSuffix(rest, "\n---"); ok {
		_ = after
		return ""
	}
	return s
}

// validateClipPath rejects path values that could be used for directory
// traversal before they reach the store's SafePath check. This is a
// defence-in-depth measure; the store already enforces ErrPathTraversal for
// any path that escapes the data root.
func validateClipPath(path string) error {
	if strings.Contains(path, "..") {
		return fmt.Errorf("path must not contain ..")
	}
	if strings.HasPrefix(path, "/") {
		return fmt.Errorf("path must not be absolute")
	}
	if strings.ContainsAny(path, "\x00\\") {
		return fmt.Errorf("path contains invalid characters")
	}
	return nil
}

// slugify converts a human title to a URL-safe lowercase slug, capped at 80
// characters and trimmed of trailing hyphens.  If the input contains no
// alphanumeric characters the function returns "clip" so that callers always
// receive a non-empty, usable slug.
func slugify(s string) string {
	s = strings.ToLower(s)
	var b strings.Builder
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		} else if r == ' ' || r == '-' {
			b.WriteRune('-')
		}
	}
	slug := b.String()
	if len(slug) > 80 {
		slug = slug[:80]
	}
	slug = strings.TrimRight(slug, "-")
	if slug == "" {
		slug = "clip"
	}
	return slug
}
