package api

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"duffel/pkg/duffellib/storage"
)

// Validate the document envelope and record graph before passing untrusted data
// to tldraw. Its browser-side schema additionally validates/migrates shape props.
// Session records are deliberately not persisted by new clients.
func validateIllustrationSnapshot(raw json.RawMessage) error {
	var snapshot map[string]any
	if err := json.Unmarshal(raw, &snapshot); err != nil || snapshot == nil {
		return errors.New("snapshot must be a tldraw document object")
	}
	document, ok := snapshot["document"].(map[string]any)
	if !ok {
		return errors.New("snapshot.document must be an object")
	}
	records, ok := document["store"].(map[string]any)
	if !ok || len(records) == 0 || len(records) > 10000 {
		return errors.New("snapshot requires 1 to 10000 document records")
	}
	schema, ok := document["schema"].(map[string]any)
	if !ok || schema["schemaVersion"] != float64(2) {
		return errors.New("snapshot requires tldraw schema version 2")
	}
	sequences, ok := schema["sequences"].(map[string]any)
	if !ok || len(sequences) == 0 {
		return errors.New("snapshot schema sequences are required")
	}
	for key, value := range sequences {
		n, ok := value.(float64)
		maximum, known := illustrationSchemaVersions[key]
		if !ok || !known || n < 0 || n > maximum || n != float64(int(n)) {
			return errors.New("invalid snapshot schema sequence")
		}
	}
	if sequences["com.tldraw.store"] == nil {
		return errors.New("snapshot store schema sequence is required")
	}
	pages := 0
	for id, value := range records {
		record, ok := value.(map[string]any)
		if !ok || record["id"] != id || !object(record["meta"]) {
			return fmt.Errorf("invalid snapshot record %q", id)
		}
		typeName, ok := record["typeName"].(string)
		if !ok || !strings.HasPrefix(id, typeName+":") {
			return fmt.Errorf("invalid snapshot record ID %q", id)
		}
		switch typeName {
		case "document":
			if id != "document:document" || !number(record["gridSize"]) || !text(record["name"]) {
				return errors.New("invalid document record")
			}
		case "page":
			pages++
			if !text(record["name"]) || !nonemptyText(record["index"]) {
				return errors.New("invalid page record")
			}
		case "user":
			if !text(record["name"]) || !text(record["color"]) || !text(record["imageUrl"]) {
				return errors.New("invalid user record")
			}
			if err := validateIllustrationAssetURL(record["imageUrl"]); err != nil {
				return err
			}
		case "shape":
			if err := validateIllustrationShape(record, records); err != nil {
				return fmt.Errorf("shape %q: %w", id, err)
			}
		case "binding":
			props, ok := record["props"].(map[string]any)
			if !ok || record["type"] != "arrow" || !recordHasType(records, record["fromId"], "shape") || !recordHasType(records, record["toId"], "shape") || !point(props["normalizedAnchor"]) || (props["terminal"] != "start" && props["terminal"] != "end") || !boolean(props["isExact"]) || !boolean(props["isPrecise"]) {
				return errors.New("invalid arrow binding")
			}
		case "asset":
			props, ok := record["props"].(map[string]any)
			if !ok || (record["type"] != "image" && record["type"] != "video" && record["type"] != "bookmark") {
				return errors.New("invalid asset record")
			}
			if record["type"] != "bookmark" && (!text(props["name"]) || !number(props["w"]) || !number(props["h"]) || !boolean(props["isAnimated"])) {
				return errors.New("invalid asset dimensions")
			}
			if record["type"] == "bookmark" {
				if !text(props["title"]) || !text(props["description"]) {
					return errors.New("invalid bookmark asset")
				}
				if err := validateIllustrationURLs(map[string]any{"url": props["src"]}, 0); err != nil {
					return err
				}
				for _, key := range []string{"image", "favicon"} {
					if !text(props[key]) {
						return errors.New("invalid bookmark image reference")
					}
					if err := validateIllustrationAssetURL(props[key]); err != nil {
						return err
					}
				}
			} else if err := validateIllustrationAssetURL(props["src"]); err != nil {
				return err
			}
		default:
			return fmt.Errorf("unsupported document record type %q", typeName)
		}
	}
	if pages == 0 || !recordHasType(records, "document:document", "document") {
		return errors.New("snapshot requires a document and a page")
	}
	// Reject parent cycles that would otherwise make graph traversal unbounded.
	for id, value := range records {
		record, _ := value.(map[string]any) // Every record was checked above.
		if record["typeName"] != "shape" {
			continue
		}
		seen := map[string]bool{id: true}
		parent, _ := record["parentId"].(string)
		for recordHasType(records, parent, "shape") {
			if len(seen) > 256 {
				return errors.New("snapshot exceeds maximum shape nesting")
			}
			if seen[parent] {
				return errors.New("snapshot has a cyclic shape hierarchy")
			}
			seen[parent] = true
			parent, _ = records[parent].(map[string]any)["parentId"].(string)
		}
	}
	if session, exists := snapshot["session"]; exists {
		state, ok := session.(map[string]any)
		if !ok || state["version"] != float64(0) || !recordHasType(records, state["currentPageId"], "page") {
			return errors.New("invalid snapshot session")
		}
		pageStates, ok := state["pageStates"].([]any)
		if !ok {
			return errors.New("invalid snapshot page states")
		}
		for _, entry := range pageStates {
			pageState, ok := entry.(map[string]any)
			if !ok || !recordHasType(records, pageState["pageId"], "page") {
				return errors.New("invalid session page")
			}
			camera, ok := pageState["camera"].(map[string]any)
			zoom, validZoom := camera["z"].(float64)
			if !ok || !point(camera) || !validZoom || zoom <= 0 {
				return errors.New("invalid session camera")
			}
			selection, ok := pageState["selectedShapeIds"].([]any)
			if !ok {
				return errors.New("invalid session selection")
			}
			for _, id := range selection {
				if !text(id) {
					return errors.New("invalid selected shape ID")
				}
			}
		}
	}
	if err := validateIllustrationURLs(snapshot, 0); err != nil {
		return err
	}
	return nil
}

var illustrationShapeProperties = map[string]string{
	"group": "", "text": "color size w font textAlign autoSize scale richText",
	"bookmark": "url w h assetId", "draw": "segments color fill dash size isComplete isClosed isPen scale",
	"geo":  "w h geo dash growY url scale color labelColor fill size font align verticalAlign richText",
	"note": "color richText size font align verticalAlign labelColor growY url scale",
	"line": "dash size color spline points scale", "frame": "w h name color",
	"arrow":     "dash size fill color labelColor bend start end arrowheadStart arrowheadEnd richText labelPosition font scale",
	"highlight": "segments color size isComplete isPen scale", "embed": "w h url",
	"image": "w h assetId playing url crop flipX flipY", "video": "w h assetId autoplay url time playing",
}

var illustrationStyleValues = map[string]string{
	"color":      "black grey light-violet violet blue light-blue yellow orange green light-green light-red red white",
	"labelColor": "black grey light-violet violet blue light-blue yellow orange green light-green light-red red white",
	"size":       "s m l xl", "font": "draw sans serif mono", "dash": "draw solid dashed dotted none",
	"fill": "none semi solid pattern fill lined-fill", "kind": "arc elbow", "spline": "line cubic",
	"align": "start middle end start-legacy middle-legacy end-legacy", "textAlign": "start middle end",
	"verticalAlign": "start middle end", "arrowheadStart": "arrow triangle square dot pipe diamond inverted bar none",
	"arrowheadEnd": "arrow triangle square dot pipe diamond inverted bar none",
	"geo":          "cloud rectangle ellipse triangle diamond pentagon hexagon octagon star rhombus rhombus-2 oval trapezoid arrow-right arrow-left arrow-up arrow-down x-box check-box heart",
}

func validateIllustrationShape(record, records map[string]any) error {
	for _, key := range []string{"x", "y", "rotation", "opacity"} {
		if !number(record[key]) {
			return fmt.Errorf("%s must be a number", key)
		}
	}
	opacity, _ := record["opacity"].(float64) // Checked above.
	if opacity < 0 || opacity > 1 || !boolean(record["isLocked"]) || !nonemptyText(record["index"]) {
		return errors.New("invalid shape fields")
	}
	if !recordHasType(records, record["parentId"], "page") && !recordHasType(records, record["parentId"], "shape") {
		return errors.New("shape parent does not exist")
	}
	props, ok := record["props"].(map[string]any)
	if !ok {
		return errors.New("shape props must be an object")
	}
	kind, _ := record["type"].(string)
	keys, ok := illustrationShapeProperties[kind]
	if !ok {
		return errors.New("unsupported shape type")
	}
	for _, key := range strings.Fields(keys) {
		if _, ok := props[key]; !ok {
			return fmt.Errorf("missing shape property %s", key)
		}
	}
	for key, value := range props {
		if choices, exists := illustrationStyleValues[key]; exists {
			s, ok := value.(string)
			if !ok || !strings.Contains(" "+choices+" ", " "+s+" ") || s == "" || strings.ContainsAny(s, " \t\r\n") {
				return fmt.Errorf("invalid shape style %s", key)
			}
		}
		switch key {
		case "w", "h", "growY", "scale", "scaleX", "scaleY", "bend", "labelPosition", "time", "elbowMidPoint", "fontSizeAdjustment":
			if !number(value) {
				return fmt.Errorf("%s must be a number", key)
			}
			if strings.HasPrefix(key, "scale") && value == float64(0) {
				return errors.New("shape scale must not be zero")
			}
		case "autoSize", "isComplete", "isClosed", "isPen", "playing", "flipX", "flipY", "autoplay":
			if !boolean(value) {
				return fmt.Errorf("%s must be a boolean", key)
			}
		case "richText":
			rich, ok := value.(map[string]any)
			if !ok || rich["type"] != "doc" || rich["content"] == nil {
				return errors.New("richText must be a document")
			}
			if err := validateIllustrationRichText(rich, 0); err != nil {
				return err
			}
		case "start", "end":
			if !point(value) {
				return errors.New("arrow endpoints must be points")
			}
		case "segments":
			segments, ok := value.([]any)
			if !ok {
				return errors.New("segments must be an array")
			}
			for _, entry := range segments {
				segment, ok := entry.(map[string]any)
				if !ok || (segment["type"] != "free" && segment["type"] != "straight") {
					return errors.New("invalid drawing segment")
				}
				if dim, exists := segment["dim"]; exists && dim != float64(2) && dim != float64(3) {
					return errors.New("invalid drawing dimensions")
				}
				if encoded, ok := segment["path"].(string); ok {
					decoded, err := base64.StdEncoding.DecodeString(encoded)
					if err != nil || (len(decoded) != 0 && (len(decoded) < 6 || len(decoded)%2 != 0)) {
						return errors.New("invalid encoded drawing path")
					}
				} else if points, ok := segment["points"].([]any); ok {
					// Pre-base64 snapshots are migrated by the browser SDK.
					for _, p := range points {
						if !point(p) {
							return errors.New("invalid drawing point")
						}
					}
				} else {
					return errors.New("drawing segment requires a path")
				}
			}
		case "points":
			points, ok := value.(map[string]any)
			if !ok {
				return errors.New("points must be an object")
			}
			for _, p := range points {
				if !point(p) {
					return errors.New("invalid line point")
				}
			}
		case "assetId":
			if value != nil && !recordHasType(records, value, "asset") {
				return errors.New("shape asset does not exist")
			}
		case "crop":
			if value != nil {
				crop, ok := value.(map[string]any)
				if !ok || !point(crop["topLeft"]) || !point(crop["bottomRight"]) {
					return errors.New("invalid image crop")
				}
			}
		case "textLastEditedBy":
			if value != nil && !text(value) {
				return errors.New("invalid note attribution")
			}
		default:
			if !text(value) {
				return fmt.Errorf("%s must be a string", key)
			}
		}
	}
	if kind == "embed" && props["url"] != "" {
		return errors.New("remote embeds are not allowed in illustrations")
	}
	return nil
}

// Supported migration sequences for the pinned tldraw SDK. Older snapshots are
// accepted for browser migration; future or foreign schemas are rejected.
var illustrationSchemaVersions = map[string]float64{
	"com.tldraw.store": 5, "com.tldraw.asset": 1, "com.tldraw.camera": 1,
	"com.tldraw.document": 2, "com.tldraw.instance": 26, "com.tldraw.instance_page_state": 5,
	"com.tldraw.page": 1, "com.tldraw.instance_presence": 6, "com.tldraw.pointer": 1,
	"com.tldraw.shape": 4, "com.tldraw.user": 1, "com.tldraw.asset.image": 6,
	"com.tldraw.asset.video": 5, "com.tldraw.asset.bookmark": 2,
	"com.tldraw.shape.group": 0, "com.tldraw.shape.text": 4, "com.tldraw.shape.bookmark": 2,
	"com.tldraw.shape.draw": 5, "com.tldraw.shape.geo": 12, "com.tldraw.shape.note": 13,
	"com.tldraw.shape.line": 5, "com.tldraw.shape.frame": 1, "com.tldraw.shape.arrow": 8,
	"com.tldraw.shape.highlight": 4, "com.tldraw.shape.embed": 4, "com.tldraw.shape.image": 5,
	"com.tldraw.shape.video": 4, "com.tldraw.binding.arrow": 1,
}

func validateIllustrationRichText(node map[string]any, depth int) error {
	if depth > 64 || !nonemptyText(node["type"]) {
		return errors.New("invalid rich-text node")
	}
	if node["type"] == "text" && !text(node["text"]) {
		return errors.New("invalid rich-text text")
	}
	if content, exists := node["content"]; exists {
		children, ok := content.([]any)
		if !ok {
			return errors.New("rich-text content must be an array")
		}
		for _, child := range children {
			childNode, ok := child.(map[string]any)
			if !ok {
				return errors.New("invalid rich-text child")
			}
			if err := validateIllustrationRichText(childNode, depth+1); err != nil {
				return err
			}
		}
	}
	if marks, exists := node["marks"]; exists {
		items, ok := marks.([]any)
		if !ok {
			return errors.New("rich-text marks must be an array")
		}
		for _, value := range items {
			mark, ok := value.(map[string]any)
			if !ok || !nonemptyText(mark["type"]) {
				return errors.New("invalid rich-text mark")
			}
		}
	}
	return nil
}

func recordHasType(records map[string]any, id any, kind string) bool {
	key, ok := id.(string)
	if !ok {
		return false
	}
	record, ok := records[key].(map[string]any)
	return ok && record["typeName"] == kind
}
func object(v any) bool       { _, ok := v.(map[string]any); return ok }
func text(v any) bool         { _, ok := v.(string); return ok }
func nonemptyText(v any) bool { s, ok := v.(string); return ok && s != "" }
func number(v any) bool       { _, ok := v.(float64); return ok }
func boolean(v any) bool      { _, ok := v.(bool); return ok }
func point(v any) bool        { p, ok := v.(map[string]any); return ok && number(p["x"]) && number(p["y"]) }

func validateIllustrationURLs(value any, depth int) error {
	if depth > 64 {
		return errors.New("snapshot exceeds maximum nesting depth")
	}
	switch value := value.(type) {
	case map[string]any:
		for key, child := range value {
			if key == "src" || key == "image" || key == "favicon" {
				if err := validateIllustrationAssetURL(child); err != nil {
					return err
				}
			}
			if key == "href" || key == "url" {
				if child != nil && !text(child) {
					return errors.New("illustration links must be strings")
				}
				if s, ok := child.(string); ok && s != "" {
					u, err := url.Parse(s)
					if err != nil || (u.Scheme != "https" && u.Scheme != "http" && u.Scheme != "" && u.Scheme != "mailto") || strings.ContainsAny(s, "\r\n\\") {
						return errors.New("unsafe illustration link")
					}
				}
			}
			if err := validateIllustrationURLs(child, depth+1); err != nil {
				return err
			}
		}
	case []any:
		for _, child := range value {
			if err := validateIllustrationURLs(child, depth+1); err != nil {
				return err
			}
		}
	}
	return nil
}

func validateIllustrationAssetURL(value any) error {
	if value == nil || value == "" {
		return nil
	}
	src, ok := value.(string)
	if !ok {
		return errors.New("asset source must be a string")
	}
	if strings.HasPrefix(src, "data:") {
		header, data, ok := strings.Cut(src, ",")
		mime := strings.TrimSuffix(strings.TrimPrefix(header, "data:"), ";base64")
		if !ok || !strings.HasSuffix(header, ";base64") {
			return errors.New("assets must use base64 raster images")
		}
		switch mime {
		case "image/png", "image/jpeg", "image/gif", "image/webp", "image/avif":
		default:
			return errors.New("unsupported embedded asset type")
		}
		decoded, err := base64.StdEncoding.DecodeString(data)
		if err != nil || len(decoded) == 0 {
			return errors.New("invalid embedded asset")
		}
		if mime != "image/avif" && http.DetectContentType(decoded) != mime {
			return errors.New("embedded asset MIME type does not match content")
		}
		return nil
	}
	u, err := url.Parse(src)
	if err != nil || u.Scheme != "" || u.Host != "" || u.Fragment != "" || !strings.HasPrefix(u.Path, "/api/fs/") || u.RawQuery != "raw=1" || strings.ContainsAny(u.Path, "\\\r\n") {
		return errors.New("assets must be embedded raster images or workspace image paths")
	}
	p, err := canonicalNotePath(strings.TrimPrefix(u.Path, "/api/fs/"))
	if err != nil || !storage.IsImagePath(p) || strings.HasSuffix(strings.ToLower(p), ".svg") {
		return errors.New("invalid workspace image asset")
	}
	return nil
}
