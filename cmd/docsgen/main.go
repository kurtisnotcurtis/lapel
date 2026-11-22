package main

import (
	"bytes"
	"flag"
	"fmt"
	"html/template"
	"os"
	"path/filepath"
	"strings"
)

// DocSpec defines a Markdown source file and its generated HTML target.
type DocSpec struct {
	SourceFile string
	OutputFile string
	NavTitle   string
	ShortTitle string
}

var defaultDocs = []DocSpec{
	{
		SourceFile: "FEATURES.md",
		OutputFile: "features.html",
		NavTitle:   "Feature Guide",
		ShortTitle: "Features",
	},
	{
		SourceFile: "COMMUNITY_DESIGN.md",
		OutputFile: "community-design.html",
		NavTitle:   "Community Design",
		ShortTitle: "Community",
	},
	{
		SourceFile: "E2E_TESTING.md",
		OutputFile: "e2e-testing.html",
		NavTitle:   "E2E Testing",
		ShortTitle: "E2E Testing",
	},
	{
		SourceFile: "WSL_DNS_ISSUE.md",
		OutputFile: "wsl-dns-issue.html",
		NavTitle:   "WSL DNS Issue",
		ShortTitle: "WSL DNS",
	},
}

type pageData struct {
	Title       string
	Description string
	SourceFile  string
	OutputFile  string
	Docs        []DocSpec
	TOC         []TOCItem
	Body        template.HTML
	HasMermaid  bool
}

var pageTmpl = template.Must(template.New("doc").Parse(`<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="UTF-8" />
  <meta name="viewport" content="width=device-width, initial-scale=1.0" />
  <title>{{.Title}} &middot; lapel</title>
  <meta name="description" content="{{.Description}}" />
  <link rel="icon" type="image/svg+xml" href="assets/lapel-logo.svg" />
  <link rel="stylesheet" href="assets/site.css" />
</head>
<body>
  <header>
    <div class="container nav">
      <a class="brand" href="index.html">
        <svg viewBox="0 0 100 100" fill="none" stroke-width="4.2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
          <path d="M 37,8 C 48.5,9.5 59.5,12.5 69,17 L 72,34 L 55.5,41 L 70,47 C 58,65 44,79 28,92 C 34.5,71 38.5,54 39.5,40.5 C 40,33.5 36.5,28.5 28,26.5 C 34.5,25 40.5,23.2 46,21.2 C 44.5,15.8 41.5,11.2 37,8 Z" />
        </svg>
        <span>lapel</span>
      </a>
      <nav class="nav-links" aria-label="Primary">
        <a href="index.html" class="hide-sm">Overview</a>
        {{- range .Docs}}
        <a href="{{.OutputFile}}"{{if eq .OutputFile $.OutputFile}} class="active"{{end}}>{{.ShortTitle}}</a>
        {{- end}}
      </nav>
    </div>
  </header>

  <main class="container doc-shell">
    <aside class="doc-sidebar" aria-label="Documentation navigation">
      <div class="doc-sidebar-group">
        <h2 class="doc-sidebar-title">Documentation</h2>
        <ul class="doc-nav-list">
          <li><a href="index.html">Home &amp; Quick Start</a></li>
          {{- range .Docs}}
          <li><a href="{{.OutputFile}}"{{if eq .OutputFile $.OutputFile}} class="active"{{end}}>{{.NavTitle}}</a></li>
          {{- end}}
        </ul>
      </div>
      {{- if .TOC}}
      <div class="doc-sidebar-group">
        <h2 class="doc-sidebar-title">On this page</h2>
        <ul class="doc-toc-list">
          {{- range .TOC}}
          <li class="toc-h{{.Level}}"><a href="#{{.ID}}">{{.Title}}</a></li>
          {{- end}}
        </ul>
      </div>
      {{- end}}
    </aside>

    <article class="doc-article">
      <div class="doc-breadcrumb">
        <a href="index.html">&larr; Back to lapel overview</a>
        <a class="doc-source-link" href="{{.SourceFile}}">View raw {{.SourceFile}}</a>
      </div>
      {{.Body}}
    </article>
  </main>

  <footer>
    <div class="container footer-inner">
      <div>&copy; 2026 lapel &middot; Licensed under Apache 2.0</div>
      <div class="footer-links">
        <a href="index.html">Home</a>
        {{- range .Docs}}
        <a href="{{.OutputFile}}">{{.NavTitle}}</a>
        {{- end}}
      </div>
    </div>
  </footer>

  <script>
    document.querySelectorAll(".copy-btn").forEach((btn) => {
      btn.addEventListener("click", async () => {
        const wrap = btn.closest(".code-wrap");
        const code = wrap ? wrap.querySelector("code") : null;
        const text = code ? code.innerText : "";
        try {
          await navigator.clipboard.writeText(text);
          const orig = btn.textContent;
          btn.textContent = "Copied!";
          setTimeout(() => { btn.textContent = orig; }, 1500);
        } catch (_) {}
      });
    });
  </script>
  {{- if .HasMermaid}}
  <script type="module">
    import mermaid from "https://cdn.jsdelivr.net/npm/mermaid@11/dist/mermaid.esm.min.mjs";
    const dark = window.matchMedia("(prefers-color-scheme: dark)").matches;
    mermaid.initialize({ startOnLoad: true, theme: dark ? "dark" : "default" });
  </script>
  {{- end}}
</body>
</html>
`))

func generateDocs(docsDir string, outDir string, specs []DocSpec) error {
	if err := os.MkdirAll(outDir, 0755); err != nil {
		return fmt.Errorf("mkdir %s: %w", outDir, err)
	}
	for _, spec := range specs {
		srcPath := filepath.Join(docsDir, spec.SourceFile)
		raw, err := os.ReadFile(srcPath)
		if err != nil {
			return fmt.Errorf("read %s: %w", srcPath, err)
		}

		res := RenderMarkdown(string(raw))
		title := res.Title
		if title == "" {
			title = spec.NavTitle
		}

		var buf bytes.Buffer
		err = pageTmpl.Execute(&buf, pageData{
			Title:       title,
			Description: fmt.Sprintf("lapel documentation: %s", title),
			SourceFile:  spec.SourceFile,
			OutputFile:  spec.OutputFile,
			Docs:        specs,
			TOC:         res.TOC,
			Body:        template.HTML(res.HTML),
			HasMermaid:  res.HasMermaid,
		})
		if err != nil {
			return fmt.Errorf("render %s: %w", spec.OutputFile, err)
		}

		outPath := filepath.Join(outDir, spec.OutputFile)
		if err := os.WriteFile(outPath, buf.Bytes(), 0644); err != nil {
			return fmt.Errorf("write %s: %w", outPath, err)
		}
		fmt.Printf("generated %s -> %s\n", srcPath, outPath)
	}
	return nil
}

func resolveDocsDir(flagVal string) string {
	if flagVal != "" {
		return flagVal
	}
	if _, err := os.Stat("docs"); err == nil {
		return "docs"
	}
	if _, err := os.Stat(filepath.Join("lapel", "docs")); err == nil {
		return filepath.Join("lapel", "docs")
	}
	return strings.TrimSpace("docs")
}

func main() {
	docsDirFlag := flag.String("docs-dir", "", "path to docs directory containing Markdown files")
	outDirFlag := flag.String("out-dir", "", "output directory for generated HTML files (defaults to docs-dir)")
	flag.Parse()

	docsDir := resolveDocsDir(*docsDirFlag)
	outDir := *outDirFlag
	if outDir == "" {
		outDir = docsDir
	}
	if err := generateDocs(docsDir, outDir, defaultDocs); err != nil {
		fmt.Fprintf(os.Stderr, "docsgen error: %v\n", err)
		os.Exit(1)
	}
}
