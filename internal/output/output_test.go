package output

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"gopkg.in/yaml.v3"
)

func TestParseFormat(t *testing.T) {
	for _, s := range []string{"table", "json", "yaml"} {
		if _, err := ParseFormat(s); err != nil {
			t.Errorf("ParseFormat(%q): %v", s, err)
		}
	}
	if _, err := ParseFormat("xml"); err == nil {
		t.Error("expected an error for an unknown format")
	}
}

func TestTableRender(t *testing.T) {
	tbl := &Table{
		Headers: []string{"name", "version", "pinned", "source"},
		Rows: [][]Cell{
			{Plain("go-task"), Plain("3.38.0"), Plain("-"), Plain("system")},
			{Plain("ripgrep"), Plain("14.1.1"), Plain("a1b2c3d"), Plain("system")},
		},
	}
	var buf bytes.Buffer
	if err := tbl.Render(&buf); err != nil {
		t.Fatal(err)
	}
	want := "NAME      VERSION   PINNED    SOURCE\n" +
		"go-task   3.38.0    -         system\n" +
		"ripgrep   14.1.1    a1b2c3d   system\n"
	if buf.String() != want {
		t.Errorf("got:\n%q\nwant:\n%q", buf.String(), want)
	}
}

func TestTableHasNoTrailingWhitespace(t *testing.T) {
	tbl := &Table{
		Headers: []string{"a", "bbbbbb"},
		Rows:    [][]Cell{{Plain("x"), Plain("y")}},
	}
	var buf bytes.Buffer
	if err := tbl.Render(&buf); err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(strings.TrimRight(buf.String(), "\n"), "\n") {
		if line != strings.TrimRight(line, " ") {
			t.Errorf("line has trailing whitespace: %q", line)
		}
	}
}

// Styling must not change column widths: escape sequences have zero display
// width, so the alignment has to be computed on the plain text.
func TestTableAlignmentIgnoresStyling(t *testing.T) {
	style := lipgloss.NewStyle().Bold(true)
	styled := &Table{
		Headers: []string{"name", "version"},
		Rows:    [][]Cell{{Styled("abc", style), Plain("1")}, {Plain("abcdef"), Plain("2")}},
	}
	plain := &Table{
		Headers: []string{"name", "version"},
		Rows:    [][]Cell{{Plain("abc"), Plain("1")}, {Plain("abcdef"), Plain("2")}},
	}
	var a, b bytes.Buffer
	_ = styled.Render(&a)
	_ = plain.Render(&b)

	stripped := strings.ReplaceAll(a.String(), "\x1b[1m", "")
	stripped = strings.ReplaceAll(stripped, "\x1b[0m", "")
	if stripped != b.String() {
		t.Errorf("styling changed the layout:\n%q\nvs\n%q", stripped, b.String())
	}
}

type row struct {
	Name      string `json:"name" yaml:"name"`
	PinnedRev string `json:"pinnedRev,omitempty" yaml:"pinnedRev,omitempty"`
	Stale     bool   `json:"stale" yaml:"stale"`
}

func TestRenderJSONFieldNames(t *testing.T) {
	var buf bytes.Buffer
	if err := Render(&buf, FormatJSON, nil, []row{{Name: "ripgrep", PinnedRev: "abc", Stale: true}}); err != nil {
		t.Fatal(err)
	}
	var back []map[string]any
	if err := json.Unmarshal(buf.Bytes(), &back); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if back[0]["pinnedRev"] != "abc" || back[0]["stale"] != true {
		t.Errorf("got %+v", back[0])
	}
}

// YAML must use the same camelCase keys as JSON, not yaml.v3's lowercased
// defaults, so scripts can switch formats freely.
func TestRenderYAMLFieldNames(t *testing.T) {
	var buf bytes.Buffer
	if err := Render(&buf, FormatYAML, nil, []row{{Name: "ripgrep", PinnedRev: "abc", Stale: true}}); err != nil {
		t.Fatal(err)
	}
	var back []map[string]any
	if err := yaml.Unmarshal(buf.Bytes(), &back); err != nil {
		t.Fatalf("invalid YAML: %v", err)
	}
	if back[0]["pinnedRev"] != "abc" || back[0]["stale"] != true {
		t.Errorf("got %+v", back[0])
	}
}

func TestRenderEmptyList(t *testing.T) {
	var buf bytes.Buffer
	if err := Render(&buf, FormatJSON, nil, []row{}); err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(buf.String()) != "[]" {
		t.Errorf("got %q, want []", buf.String())
	}
}

func TestOrDash(t *testing.T) {
	if OrDash("") != "-" || OrDash("x") != "x" {
		t.Error("OrDash is wrong")
	}
}
