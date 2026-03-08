package api

import (
	"encoding/json"
	"net/url"
	"reflect"
	"testing"

	"duffel/src/backend/internal/search"
)

func TestParseSearchFields(t *testing.T) {
	t.Run("empty", func(t *testing.T) {
		fields, err := parseSearchFields("")
		if err != nil {
			t.Fatalf("parseSearchFields returned error: %v", err)
		}
		if fields != nil {
			t.Fatalf("fields = %#v, want nil", fields)
		}
	})

	t.Run("valid with dedupe and spaces", func(t *testing.T) {
		fields, err := parseSearchFields(" path ,title,path, modified_at, explain ")
		if err != nil {
			t.Fatalf("parseSearchFields returned error: %v", err)
		}
		want := []string{"path", "title", "modified_at", "explain"}
		if !reflect.DeepEqual(fields, want) {
			t.Fatalf("fields = %#v, want %#v", fields, want)
		}
	})

	t.Run("invalid", func(t *testing.T) {
		_, err := parseSearchFields("path,bad")
		if err == nil {
			t.Fatal("parseSearchFields error = nil, want non-nil")
		}
	})
}

func TestParseSearchRequest(t *testing.T) {
	t.Run("parses hybrid options", func(t *testing.T) {
		params := url.Values{
			"q":               []string{"performance"},
			"limit":           []string{"12"},
			"offset":          []string{"4"},
			"intent":          []string{"runtime profiling"},
			"candidate_limit": []string{"80"},
			"min_score":       []string{"0.25"},
			"explain":         []string{"true"},
			"fields":          []string{"path,title,explain"},
		}

		opts, fields, err := parseSearchRequest(params)
		if err != nil {
			t.Fatalf("parseSearchRequest error = %v", err)
		}

		wantOpts := search.SearchOptions{
			Query:          "performance",
			Collection:     "duffel",
			Limit:          12,
			Offset:         4,
			Intent:         "runtime profiling",
			CandidateLimit: 80,
			MinScore:       0.25,
			Explain:        true,
		}
		if !reflect.DeepEqual(opts, wantOpts) {
			t.Fatalf("opts = %#v, want %#v", opts, wantOpts)
		}
		wantFields := []string{"path", "title", "explain"}
		if !reflect.DeepEqual(fields, wantFields) {
			t.Fatalf("fields = %#v, want %#v", fields, wantFields)
		}
	})

	t.Run("rejects legacy params", func(t *testing.T) {
		legacy := []string{"sort", "prefix", "after", "before"}
		for _, key := range legacy {
			params := url.Values{"q": []string{"test"}, key: []string{"x"}}
			if _, _, err := parseSearchRequest(params); err == nil {
				t.Fatalf("expected error for legacy param %q", key)
			}
		}
	})

	t.Run("rejects invalid explain", func(t *testing.T) {
		params := url.Values{"q": []string{"test"}, "explain": []string{"maybe"}}
		if _, _, err := parseSearchRequest(params); err == nil {
			t.Fatal("expected parse error for explain")
		}
	})
}

func TestProjectSearchResults(t *testing.T) {
	results := []search.Result{
		{
			Path:       "notes/a.md",
			Title:      "A",
			Snippet:    "snippet",
			Score:      1.23,
			ModifiedAt: "2026-02-20T10:00:00Z",
			Explain:    []byte(`{"rrf":{"rank":1}}`),
		},
	}

	got := projectSearchResults(results, []string{"path", "title", "explain"})
	if len(got) != 1 {
		t.Fatalf("len(got) = %d, want 1", len(got))
	}
	if len(got[0]) != 3 {
		t.Fatalf("len(got[0]) = %d, want 3", len(got[0]))
	}
	if got[0]["path"] != "notes/a.md" {
		t.Fatalf("path = %v, want notes/a.md", got[0]["path"])
	}
	if got[0]["title"] != "A" {
		t.Fatalf("title = %v, want A", got[0]["title"])
	}
	explain, ok := got[0]["explain"].(json.RawMessage)
	if !ok {
		t.Fatalf("explain type = %T, want json.RawMessage", got[0]["explain"])
	}
	if string(explain) != `{"rrf":{"rank":1}}` {
		t.Fatalf("explain = %s, want JSON payload", string(explain))
	}
}
