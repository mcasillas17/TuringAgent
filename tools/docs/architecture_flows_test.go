package docs

import (
	"bytes"
	"encoding/json"
	"fmt"
	"html"
	"io/fs"
	"net/url"
	"os"
	"path"
	"reflect"
	"regexp"
	"strings"
	"testing"
	"testing/fstest"

	mdast "github.com/yuin/goldmark/ast"
)

const (
	flowGuide    = "docs/architecture/system-flows.md"
	flowManifest = "docs/architecture/turing-flow.hero.json"
	flowTemplate = "docs/architecture/turing-flow.template.html"
	flowHTML     = "docs/architecture/turing-flow.html"
	heroToken    = "<!-- ARCHFLOW_HERO -->"
)

var (
	heroSVG      = regexp.MustCompile(`(?s)<svg\b[^>]*data-archflow-hero="1".*?</svg>`)
	heroChapters = regexp.MustCompile(`(?s)<metadata id="afh-chapters">(.*?)</metadata>`)
)

func TestArchitectureFlowArtifacts(t *testing.T) {
	if err := checkArchitectureFlowArtifacts(os.DirFS("../..")); err != nil {
		t.Fatal(err)
	}
}

func checkArchitectureFlowArtifacts(files fs.FS) error {
	var content [4][]byte
	for index, name := range []string{flowManifest, flowTemplate, flowHTML, flowGuide} {
		data, err := fs.ReadFile(files, name)
		if err != nil {
			return fmt.Errorf("read %s: %w", name, err)
		}
		content[index] = data
	}
	manifest, template, artifact, guide := content[0], content[1], content[2], content[3]
	var model struct {
		Entities []struct {
			ID       string `json:"id"`
			Evidence []struct {
				Path      string `json:"path"`
				StartLine int    `json:"start_line"`
				EndLine   int    `json:"end_line"`
			} `json:"evidence"`
		} `json:"entities"`
		Flows []struct {
			ID string `json:"id"`
		} `json:"flows"`
		Chapters json.RawMessage `json:"chapters"`
	}
	if err := json.Unmarshal(manifest, &model); err != nil {
		return fmt.Errorf("decode flow manifest: %w", err)
	}
	if len(model.Entities) == 0 || len(model.Flows) == 0 {
		return fmt.Errorf("flow manifest needs entities and flows")
	}
	if len(heroSVG.FindAll(artifact, -1)) != 1 || bytes.Count(template, []byte(heroToken)) != 1 {
		return fmt.Errorf("expected one generated hero and one template placeholder")
	}
	if !bytes.Equal(heroSVG.ReplaceAll(artifact, []byte(heroToken)), template) {
		return fmt.Errorf("generated HTML differs from template; regenerate %s", flowHTML)
	}
	embedded := heroChapters.FindAllSubmatch(artifact, -1)
	if len(embedded) != 1 {
		return fmt.Errorf("expected one embedded chapter definition")
	}
	var want, got []map[string]any
	if err := json.Unmarshal(model.Chapters, &want); err != nil {
		return fmt.Errorf("decode manifest chapters: %w", err)
	}
	if err := json.Unmarshal([]byte(html.UnescapeString(string(embedded[0][1]))), &got); err != nil {
		return fmt.Errorf("decode embedded chapters: %w", err)
	}
	if len(want) < 4 || len(want) > 8 || !reflect.DeepEqual(want, got) {
		return fmt.Errorf("embedded chapters differ from the manifest or are outside the 4-8 chapter bound")
	}
	for _, entity := range model.Entities {
		if entity.ID == "" || !bytes.Contains(artifact, []byte(`data-af-entity="`+entity.ID+`"`)) {
			return fmt.Errorf("missing rendered entity %q", entity.ID)
		}
		if len(entity.Evidence) == 0 {
			return fmt.Errorf("entity %s needs source evidence", entity.ID)
		}
		for _, evidence := range entity.Evidence {
			if !fs.ValidPath(evidence.Path) {
				return fmt.Errorf("invalid evidence path for %s", entity.ID)
			}
			source, err := fs.ReadFile(files, evidence.Path)
			if err != nil {
				return fmt.Errorf("read evidence for %s: %w", entity.ID, err)
			}
			end := evidence.EndLine
			if end == 0 {
				end = evidence.StartLine
			}
			if evidence.StartLine < 1 || end < evidence.StartLine || end > len(bytes.Split(source, []byte("\n"))) {
				return fmt.Errorf("invalid evidence range for %s in %s", entity.ID, evidence.Path)
			}
		}
	}
	for _, flow := range model.Flows {
		if flow.ID == "" || !bytes.Contains(artifact, []byte(`data-af-flow="`+flow.ID+`"`)) {
			return fmt.Errorf("missing rendered flow %q", flow.ID)
		}
	}
	markdown := parseStatusMarkdown(string(guide))
	return mdast.Walk(markdown.document, func(node mdast.Node, entering bool) (mdast.WalkStatus, error) {
		link, ok := node.(*mdast.Link)
		if !entering || !ok {
			return mdast.WalkContinue, nil
		}
		destination, err := url.Parse(string(link.Destination))
		if err != nil {
			return mdast.WalkStop, fmt.Errorf("invalid flow guide link: %w", err)
		}
		if destination.IsAbs() || destination.Host != "" || destination.Path == "" {
			return mdast.WalkContinue, nil
		}
		target := path.Join(path.Dir(flowGuide), destination.Path)
		if !fs.ValidPath(target) {
			return mdast.WalkStop, fmt.Errorf("flow guide link escapes the repository: %s", destination.Path)
		}
		if _, err := fs.Stat(files, target); err != nil {
			return mdast.WalkStop, fmt.Errorf("unresolved flow guide link %s: %w", destination.Path, err)
		}
		return mdast.WalkContinue, nil
	})
}

func TestArchitectureFlowArtifactDrift(t *testing.T) {
	chapters := `[{"id":"one"},{"id":"two"},{"id":"three"},{"id":"four"}]`
	svg := `<svg data-archflow-hero="1"><metadata id="afh-chapters">` + html.EscapeString(chapters) +
		`</metadata><g data-af-entity="node"/><g data-af-flow="edge"/></svg>`
	template := "<html>" + heroToken + "</html>"
	manifest := `{"entities":[{"id":"node","evidence":[{"path":"source.go","start_line":1,"end_line":2}]}],` +
		`"flows":[{"id":"edge"}],"chapters":` + chapters + `}`
	for _, fixture := range []struct {
		name, file, before, after, problem string
	}{
		{"unchanged", "", "", "", ""},
		{"HTML edit", flowHTML, "</html>", "extra</html>", "differs from template"},
		{"template edit", flowTemplate, "</html>", "extra</html>", "differs from template"},
		{"chapter edit", flowManifest, `"one"`, `"changed"`, "embedded chapters differ"},
		{"missing entity", flowHTML, `data-af-entity="node"`, `data-af-entity="other"`, "missing rendered entity"},
		{"missing flow", flowHTML, `data-af-flow="edge"`, `data-af-flow="other"`, "missing rendered flow"},
		{"missing source", flowManifest, "source.go", "absent.go", "read evidence"},
		{"outside source", flowManifest, "source.go", "../source.go", "invalid evidence path"},
		{"range drift", flowManifest, `"end_line":2`, `"end_line":20`, "invalid evidence range"},
		{"broken link", flowGuide, "../../README.md", "../../missing.md", "unresolved flow guide link"},
		{"escaping link", flowGuide, "../../README.md", "../../../outside.md", "link escapes"},
		{"code example", flowGuide, "[Readme]", "`[fake](missing.md)`\n\n[Readme]", ""},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			files := fstest.MapFS{
				flowManifest: {Data: []byte(manifest)},
				flowTemplate: {Data: []byte(template)},
				flowHTML:     {Data: []byte(strings.Replace(template, heroToken, svg, 1))},
				flowGuide:    {Data: []byte("[Readme](../../README.md)\n[External](https://example.invalid)")},
				"source.go":  {Data: []byte("first\nsecond\n")},
				"README.md":  {Data: []byte("readme")},
			}
			if fixture.file != "" {
				data := string(files[fixture.file].Data)
				if !strings.Contains(data, fixture.before) {
					t.Fatal("fixture did not match its baseline")
				}
				files[fixture.file].Data = []byte(strings.Replace(data, fixture.before, fixture.after, 1))
			}
			err := checkArchitectureFlowArtifacts(files)
			if fixture.problem == "" {
				if err != nil {
					t.Fatal(err)
				}
			} else if err == nil || !strings.Contains(err.Error(), fixture.problem) {
				t.Fatalf("error = %v, want %q", err, fixture.problem)
			}
		})
	}
}
