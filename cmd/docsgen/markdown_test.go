package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRenderMarkdownFeatures(t *testing.T) {
	md := `# Main Title

Intro with **bold**, *italic*, **VS Code plugin sync *(in progress)***, **` + "`runWslWith()`" + `**, snake_case_id, and ` + "`inline_code`" + `.

## Overview & Links

See [Feature Guide](FEATURES.md#core) and [GitHub](https://github.com/kurtisnotcurtis/lapel).

| Step | Command | Notes |
|------|:-------:|------:|
| 1 | ` + "`lapel doctor`" + ` | Ready |

- [x] Done item
- [ ] Open item
  - Nested item

` + "```bash\nlapel setup\n```" + `

` + "```mermaid\ngraph TD\n    A --> B\n```" + `
`

	res := RenderMarkdown(md)
	if res.Title != "Main Title" {
		t.Fatalf("expected title 'Main Title', got %q", res.Title)
	}
	if !res.HasMermaid {
		t.Fatalf("expected HasMermaid to be true")
	}
	if len(res.TOC) != 1 || res.TOC[0].ID != "overview-links" {
		t.Fatalf("unexpected TOC: %+v", res.TOC)
	}

	checks := []string{
		`<h1 id="main-title">Main Title</h1>`,
		`<strong>bold</strong>`,
		`<em>italic</em>`,
		`<strong>VS Code plugin sync <em>(in progress)</em></strong>`,
		`<strong><code>runWslWith()</code></strong>`,
		`snake_case_id`,
		`<code>inline_code</code>`,
		`<a href="features.html#core">Feature Guide</a>`,
		`<a href="https://github.com/kurtisnotcurtis/lapel">GitHub</a>`,
		`<th style="text-align:center">Command</th>`,
		`<td style="text-align:right">Ready</td>`,
		`<input type="checkbox" disabled checked />`,
		`<input type="checkbox" disabled />`,
		`<ul>`,
		`<pre><code class="language-bash">lapel setup</code></pre>`,
		`<pre class="mermaid">graph TD`,
	}
	for _, want := range checks {
		if !strings.Contains(res.HTML, want) {
			t.Errorf("rendered HTML missing %q\nGot:\n%s", want, res.HTML)
		}
	}
}

func TestGenerateDocsOnWorkspaceDocs(t *testing.T) {
	docsDir := filepath.Join("..", "..", "docs")
	if _, err := os.Stat(docsDir); err != nil {
		t.Skipf("docs directory not found at %s: %v", docsDir, err)
	}

	tmpDir := t.TempDir()
	if err := generateDocs(docsDir, tmpDir, defaultDocs); err != nil {
		t.Fatalf("generateDocs failed: %v", err)
	}

	for _, spec := range defaultDocs {
		outBytes, err := os.ReadFile(filepath.Join(tmpDir, spec.OutputFile))
		if err != nil {
			t.Fatalf("read output %s: %v", spec.OutputFile, err)
		}
		out := string(outBytes)
		if !strings.Contains(out, `<link rel="stylesheet" href="assets/site.css" />`) {
			t.Errorf("%s missing site.css stylesheet link", spec.OutputFile)
		}
		if !strings.Contains(out, `class="doc-article"`) {
			t.Errorf("%s missing doc-article container", spec.OutputFile)
		}
		if spec.SourceFile == "FEATURES.md" && !strings.Contains(out, "mermaid.esm.min.mjs") {
			t.Errorf("features.html should include mermaid script")
		}
		if spec.SourceFile != "FEATURES.md" && strings.Contains(out, "mermaid.esm.min.mjs") {
			t.Errorf("%s should not include mermaid script", spec.OutputFile)
		}
	}
}
