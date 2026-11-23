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
        <svg viewBox="0 0 100 100" stroke-width="4.2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
          <defs>
            <linearGradient id="lapel-bolt-grad" x1="0%" y1="0%" x2="0%" y2="100%">
              <stop offset="0%" stop-color="#FFB952" />
              <stop offset="50%" stop-color="#FA8546" />
              <stop offset="100%" stop-color="#F2595B" />
            </linearGradient>
            <linearGradient id="lapel-bolt-fill" x1="0%" y1="0%" x2="0%" y2="100%">
              <stop offset="0%" stop-color="#FFB952" stop-opacity="0.28" />
              <stop offset="50%" stop-color="#FA8546" stop-opacity="0.28" />
              <stop offset="100%" stop-color="#F2595B" stop-opacity="0.28" />
            </linearGradient>
          </defs>
          <path d="M 37,8 C 48.5,9.5 59.5,12.5 69,17 L 72,34 L 55.5,41 L 70,47 C 58,65 44,79 28,92 C 34.5,71 39.2,53 39.5,39.5 C 39.6,33 38.4,28 36.4,24.2 L 46,21.2 C 44.5,15.8 41.5,11.2 37,8 Z" />
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
  <div class="mermaid-modal" id="mermaid-modal" role="dialog" aria-modal="true" aria-label="Expanded diagram viewer">
    <div class="mermaid-modal-dialog">
      <div class="mermaid-toolbar">
        <span class="mermaid-toolbar-label">Diagram viewer <span class="mermaid-toolbar-hint">&middot; Drag to pan &middot; Ctrl+scroll to zoom &middot; Esc to close</span></span>
        <div class="mermaid-controls">
          <button type="button" class="mermaid-btn" data-modal-action="out" title="Zoom out" aria-label="Zoom out">&minus;</button>
          <button type="button" class="mermaid-btn mermaid-btn-zoom-level" data-modal-action="reset" title="Reset zoom">100%</button>
          <button type="button" class="mermaid-btn" data-modal-action="in" title="Zoom in" aria-label="Zoom in">+</button>
          <button type="button" class="mermaid-btn" data-modal-action="fit" title="Fit diagram to window">Fit</button>
          <button type="button" class="mermaid-btn" data-modal-action="close" title="Close (Esc)" aria-label="Close expanded diagram">&times; Close</button>
        </div>
      </div>
      <div class="mermaid-modal-viewport" id="mermaid-modal-viewport">
        <div class="mermaid-modal-canvas" id="mermaid-modal-canvas"></div>
      </div>
    </div>
  </div>
  <script type="module">
    import mermaid from "https://cdn.jsdelivr.net/npm/mermaid@11/dist/mermaid.esm.min.mjs";
    const dark = window.matchMedia("(prefers-color-scheme: dark)").matches;
    mermaid.initialize({
      startOnLoad: false,
      theme: dark ? "dark" : "default",
      themeVariables: { fontSize: "15px" },
      flowchart: { useMaxWidth: true, htmlLabels: true, curve: "basis", padding: 16 }
    });
    await mermaid.run({ querySelector: ".mermaid" });

    function getNaturalSize(svg) {
      const vb = svg.viewBox && svg.viewBox.baseVal;
      if (vb && vb.width > 0 && vb.height > 0) {
        return { width: vb.width, height: vb.height };
      }
      const rect = svg.getBoundingClientRect();
      return { width: Math.max(rect.width, 400), height: Math.max(rect.height, 240) };
    }

    function enableDragPan(viewport) {
      let dragging = false;
      let startX = 0, startY = 0, scrollLeft = 0, scrollTop = 0;
      viewport.addEventListener("pointerdown", (e) => {
        if (e.button !== 0 || !viewport.classList.contains("is-zoomed")) return;
        dragging = true;
        startX = e.clientX;
        startY = e.clientY;
        scrollLeft = viewport.scrollLeft;
        scrollTop = viewport.scrollTop;
        viewport.classList.add("is-dragging");
        viewport.setPointerCapture(e.pointerId);
      });
      viewport.addEventListener("pointermove", (e) => {
        if (!dragging) return;
        viewport.scrollLeft = scrollLeft - (e.clientX - startX);
        viewport.scrollTop = scrollTop - (e.clientY - startY);
      });
      const stop = () => {
        if (!dragging) return;
        dragging = false;
        viewport.classList.remove("is-dragging");
      };
      viewport.addEventListener("pointerup", stop);
      viewport.addEventListener("pointercancel", stop);
    }

    const modal = document.getElementById("mermaid-modal");
    const modalViewport = document.getElementById("mermaid-modal-viewport");
    const modalCanvas = document.getElementById("mermaid-modal-canvas");
    const modalLevelBtn = modal.querySelector('[data-modal-action="reset"]');
    let modalSvg = null;
    let modalNat = { width: 800, height: 600 };
    let modalScale = 1;

    function applyModalScale(scale) {
      if (!modalSvg) return;
      modalScale = Math.min(4, Math.max(0.4, scale));
      const w = Math.round(modalNat.width * modalScale);
      const h = Math.round(modalNat.height * modalScale);
      modalSvg.style.width = w + "px";
      modalSvg.style.height = h + "px";
      modalLevelBtn.textContent = Math.round(modalScale * 100) + "%";
      const overflows = w > modalViewport.clientWidth - 40 || h > modalViewport.clientHeight - 40;
      modalViewport.classList.toggle("is-zoomed", overflows);
    }

    function fitModal() {
      if (!modalSvg) return;
      const availW = Math.max(200, modalViewport.clientWidth - 56);
      const availH = Math.max(200, modalViewport.clientHeight - 56);
      const fit = Math.min(availW / modalNat.width, availH / modalNat.height, 2.2);
      applyModalScale(fit);
    }

    function openModal(sourceSvg) {
      modalNat = getNaturalSize(sourceSvg);
      modalCanvas.innerHTML = "";
      modalSvg = sourceSvg.cloneNode(true);
      modalSvg.removeAttribute("style");
      modalSvg.style.maxWidth = "none";
      modalCanvas.appendChild(modalSvg);
      modal.classList.add("open");
      document.body.style.overflow = "hidden";
      requestAnimationFrame(fitModal);
    }

    function closeModal() {
      modal.classList.remove("open");
      document.body.style.overflow = "";
      modalCanvas.innerHTML = "";
      modalSvg = null;
    }

    enableDragPan(modalViewport);
    modalViewport.addEventListener("wheel", (e) => {
      if (!e.ctrlKey && !e.metaKey) return;
      e.preventDefault();
      applyModalScale(modalScale + (e.deltaY < 0 ? 0.2 : -0.2));
    }, { passive: false });

    modal.addEventListener("click", (e) => {
      if (e.target === modal) closeModal();
      const btn = e.target.closest("[data-modal-action]");
      if (!btn) return;
      const action = btn.getAttribute("data-modal-action");
      if (action === "in") applyModalScale(modalScale + 0.25);
      else if (action === "out") applyModalScale(modalScale - 0.25);
      else if (action === "reset") applyModalScale(1);
      else if (action === "fit") fitModal();
      else if (action === "close") closeModal();
    });

    document.addEventListener("keydown", (e) => {
      if (!modal.classList.contains("open")) return;
      if (e.key === "Escape") closeModal();
      else if (e.key === "+" || e.key === "=") applyModalScale(modalScale + 0.25);
      else if (e.key === "-") applyModalScale(modalScale - 0.25);
      else if (e.key === "0") fitModal();
    });

    document.querySelectorAll(".mermaid-wrap").forEach((wrap) => {
      const pre = wrap.querySelector("pre.mermaid");
      const svg = pre ? pre.querySelector("svg") : null;
      if (!svg) return;

      const nat = getNaturalSize(svg);
      const toolbar = document.createElement("div");
      toolbar.className = "mermaid-toolbar";
      toolbar.innerHTML =
        '<span class="mermaid-toolbar-label">Diagram <span class="mermaid-toolbar-hint">&middot; Click Expand or zoom to inspect</span></span>' +
        '<div class="mermaid-controls">' +
        '  <button type="button" class="mermaid-btn" data-action="out" title="Zoom out" aria-label="Zoom out">&minus;</button>' +
        '  <button type="button" class="mermaid-btn mermaid-btn-zoom-level" data-action="fit" title="Reset to fit">Fit</button>' +
        '  <button type="button" class="mermaid-btn" data-action="in" title="Zoom in" aria-label="Zoom in">+</button>' +
        '  <button type="button" class="mermaid-btn" data-action="expand" title="Open fullscreen diagram viewer" aria-label="Expand diagram">&#x2922; Expand</button>' +
        '</div>';

      const viewport = document.createElement("div");
      viewport.className = "mermaid-viewport";
      const stage = document.createElement("div");
      stage.className = "mermaid-stage";
      pre.parentNode.insertBefore(toolbar, pre);
      pre.parentNode.insertBefore(viewport, pre);
      stage.appendChild(pre);
      viewport.appendChild(stage);

      const levelBtn = toolbar.querySelector('[data-action="fit"]');
      let scale = null; // null == fit to container width

      function applyInlineZoom(nextScale) {
        if (nextScale === null) {
          scale = null;
          svg.style.width = "";
          svg.style.height = "";
          svg.style.maxWidth = "100%";
          viewport.classList.remove("is-zoomed");
          levelBtn.textContent = "Fit";
          return;
        }
        scale = Math.min(3, Math.max(0.5, nextScale));
        svg.style.maxWidth = "none";
        svg.style.width = Math.round(nat.width * scale) + "px";
        svg.style.height = Math.round(nat.height * scale) + "px";
        viewport.classList.add("is-zoomed");
        levelBtn.textContent = Math.round(scale * 100) + "%";
      }

      function currentEffectiveScale() {
        if (scale !== null) return scale;
        const rect = svg.getBoundingClientRect();
        return rect.width > 0 ? rect.width / nat.width : 1;
      }

      enableDragPan(viewport);

      viewport.addEventListener("click", (e) => {
        if (!viewport.classList.contains("is-zoomed")) {
          openModal(svg);
        }
      });

      viewport.addEventListener("wheel", (e) => {
        if (!e.ctrlKey && !e.metaKey) return;
        e.preventDefault();
        applyInlineZoom(currentEffectiveScale() + (e.deltaY < 0 ? 0.25 : -0.25));
      }, { passive: false });

      toolbar.addEventListener("click", (e) => {
        const btn = e.target.closest("[data-action]");
        if (!btn) return;
        const action = btn.getAttribute("data-action");
        if (action === "in") applyInlineZoom(currentEffectiveScale() + 0.25);
        else if (action === "out") applyInlineZoom(currentEffectiveScale() - 0.25);
        else if (action === "fit") applyInlineZoom(null);
        else if (action === "expand") openModal(svg);
      });
    });
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
