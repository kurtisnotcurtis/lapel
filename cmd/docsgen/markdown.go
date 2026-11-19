package main

import (
	"fmt"
	"html"
	"path/filepath"
	"regexp"
	"strings"
	"unicode"
)

// TOCItem represents an entry in a document's table of contents.
type TOCItem struct {
	Level int
	Title string
	ID    string
}

// RenderResult holds the rendered HTML body and metadata extracted from Markdown.
type RenderResult struct {
	Title      string
	HTML       string
	TOC        []TOCItem
	HasMermaid bool
}

var docLinkMap = map[string]string{
	"FEATURES.md":         "features.html",
	"COMMUNITY_DESIGN.md": "community-design.html",
	"E2E_TESTING.md":      "e2e-testing.html",
	"WSL_DNS_ISSUE.md":    "wsl-dns-issue.html",
}

var (
	reHeading    = regexp.MustCompile(`^(#{1,6})\s+(.+?)(?:\s+#+)?\s*$`)
	reHR         = regexp.MustCompile(`^\s*(?:[-*_]\s*){3,}$`)
	reOrdered    = regexp.MustCompile(`^(\s*)(\d+)\.\s+(.*)$`)
	reUnordered  = regexp.MustCompile(`^(\s*)[-*+]\s+(.*)$`)
	reTaskPrefix = regexp.MustCompile(`^\[([ xX])\]\s+(.*)$`)
	reInlineCode = regexp.MustCompile("(`+)([^`]+?)`+")
	reImage      = regexp.MustCompile(`!\[([^\]]*)\]\(([^)\s]+)(?:\s+"([^"]*)")?\)`)
	reLink       = regexp.MustCompile(`\[([^\]]+)\]\(([^)\s]+)(?:\s+"([^"]*)")?\)`)
	reStrike     = regexp.MustCompile(`~~([^\s~](?:.*?[^\s~])?)~~`)
)

// RenderMarkdown converts GitHub-Flavored Markdown into HTML suitable for static doc pages.
func RenderMarkdown(src string) RenderResult {
	normalized := strings.ReplaceAll(src, "\r\n", "\n")
	lines := strings.Split(normalized, "\n")

	var out strings.Builder
	var toc []TOCItem
	var title string
	hasMermaid := false
	slugCounts := make(map[string]int)

	i := 0
	for i < len(lines) {
		line := lines[i]
		trimmed := strings.TrimSpace(line)

		if trimmed == "" {
			i++
			continue
		}

		// Fenced code block
		if fence, lang, ok := parseFenceStart(line); ok {
			i++
			var codeLines []string
			for i < len(lines) {
				if isFenceEnd(lines[i], fence) {
					i++
					break
				}
				codeLines = append(codeLines, lines[i])
				i++
			}
			code := strings.Join(codeLines, "\n")
			if strings.EqualFold(lang, "mermaid") {
				hasMermaid = true
				out.WriteString(`<div class="mermaid-wrap"><pre class="mermaid">`)
				out.WriteString(html.EscapeString(code))
				out.WriteString("</pre></div>\n")
			} else {
				out.WriteString(`<div class="code-wrap">`)
				if lang != "" {
					out.WriteString(`<span class="code-lang">`)
					out.WriteString(html.EscapeString(lang))
					out.WriteString(`</span>`)
				}
				out.WriteString(`<button class="copy-btn" type="button">Copy</button><pre><code`)
				if lang != "" {
					out.WriteString(` class="language-`)
					out.WriteString(html.EscapeString(lang))
					out.WriteString(`"`)
				}
				out.WriteString(`>`)
				out.WriteString(html.EscapeString(code))
				out.WriteString("</code></pre></div>\n")
			}
			continue
		}

		// Heading
		if m := reHeading.FindStringSubmatch(line); m != nil {
			level := len(m[1])
			rawHeading := strings.TrimSpace(m[2])
			plainHeading := stripInlineFormatting(rawHeading)
			if level == 1 && title == "" {
				title = plainHeading
			}
			id := uniqueSlug(plainHeading, slugCounts)
			if level == 2 || level == 3 {
				toc = append(toc, TOCItem{
					Level: level,
					Title: plainHeading,
					ID:    id,
				})
			}
			out.WriteString(fmt.Sprintf(`<h%d id="%s">%s`, level, html.EscapeString(id), renderInline(rawHeading)))
			if level >= 2 && level <= 4 {
				out.WriteString(fmt.Sprintf(` <a class="heading-anchor" href="#%s" aria-label="Link to %s">#</a>`,
					html.EscapeString(id), html.EscapeString(plainHeading)))
			}
			out.WriteString(fmt.Sprintf("</h%d>\n", level))
			i++
			continue
		}

		// Horizontal rule
		if reHR.MatchString(line) && !strings.HasPrefix(trimmed, "|") {
			out.WriteString("<hr />\n")
			i++
			continue
		}

		// Blockquote
		if strings.HasPrefix(trimmed, ">") {
			var quoteLines []string
			for i < len(lines) {
				qt := strings.TrimSpace(lines[i])
				if !strings.HasPrefix(qt, ">") {
					break
				}
				unquoted := strings.TrimPrefix(qt, ">")
				unquoted = strings.TrimPrefix(unquoted, " ")
				quoteLines = append(quoteLines, unquoted)
				i++
			}
			inner := RenderMarkdown(strings.Join(quoteLines, "\n"))
			out.WriteString("<blockquote>\n")
			out.WriteString(inner.HTML)
			out.WriteString("</blockquote>\n")
			continue
		}

		// GFM Pipe Table
		if i+1 < len(lines) && looksLikeTableRow(line) && isTableSeparator(lines[i+1]) {
			tableHTML, nextIdx := parseTable(lines, i)
			out.WriteString(tableHTML)
			i = nextIdx
			continue
		}

		// Unordered or Ordered List
		if isListItem(line) {
			listHTML, nextIdx := parseList(lines, i)
			out.WriteString(listHTML)
			i = nextIdx
			continue
		}

		// Paragraph
		var paraLines []string
		for i < len(lines) {
			cur := lines[i]
			curTrim := strings.TrimSpace(cur)
			if curTrim == "" ||
				reHeading.MatchString(cur) ||
				reHR.MatchString(cur) ||
				strings.HasPrefix(curTrim, ">") ||
				isListItem(cur) {
				break
			}
			if _, _, ok := parseFenceStart(cur); ok {
				break
			}
			if i+1 < len(lines) && looksLikeTableRow(cur) && isTableSeparator(lines[i+1]) {
				break
			}
			paraLines = append(paraLines, curTrim)
			i++
		}
		out.WriteString("<p>")
		out.WriteString(renderInline(strings.Join(paraLines, " ")))
		out.WriteString("</p>\n")
	}

	return RenderResult{
		Title:      title,
		HTML:       out.String(),
		TOC:        toc,
		HasMermaid: hasMermaid,
	}
}

func parseFenceStart(line string) (fence string, lang string, ok bool) {
	trimmed := strings.TrimSpace(line)
	for _, ch := range []byte{'`', '~'} {
		if len(trimmed) >= 3 && trimmed[0] == ch && trimmed[1] == ch && trimmed[2] == ch {
			n := 0
			for n < len(trimmed) && trimmed[n] == ch {
				n++
			}
			fence = strings.Repeat(string(ch), n)
			lang = strings.TrimSpace(trimmed[n:])
			if fields := strings.Fields(lang); len(fields) > 0 {
				lang = fields[0]
			}
			return fence, lang, true
		}
	}
	return "", "", false
}

func isFenceEnd(line string, fence string) bool {
	trimmed := strings.TrimSpace(line)
	if len(trimmed) < len(fence) {
		return false
	}
	ch := fence[0]
	for i := 0; i < len(trimmed); i++ {
		if trimmed[i] != ch {
			return false
		}
	}
	return true
}

func looksLikeTableRow(line string) bool {
	trimmed := strings.TrimSpace(line)
	return strings.Contains(trimmed, "|")
}

func isTableSeparator(line string) bool {
	cells := splitTableRow(line)
	if len(cells) == 0 {
		return false
	}
	for _, c := range cells {
		c = strings.TrimSpace(c)
		if c == "" {
			return false
		}
		c = strings.TrimPrefix(c, ":")
		c = strings.TrimSuffix(c, ":")
		if len(c) < 1 {
			return false
		}
		for _, r := range c {
			if r != '-' {
				return false
			}
		}
	}
	return true
}

func splitTableRow(line string) []string {
	trimmed := strings.TrimSpace(line)
	trimmed = strings.TrimPrefix(trimmed, "|")
	trimmed = strings.TrimSuffix(trimmed, "|")
	var cells []string
	var cur strings.Builder
	escaped := false
	for i := 0; i < len(trimmed); i++ {
		ch := trimmed[i]
		if escaped {
			cur.WriteByte(ch)
			escaped = false
			continue
		}
		if ch == '\\' {
			escaped = true
			cur.WriteByte(ch)
			continue
		}
		if ch == '|' {
			cells = append(cells, strings.TrimSpace(cur.String()))
			cur.Reset()
			continue
		}
		cur.WriteByte(ch)
	}
	cells = append(cells, strings.TrimSpace(cur.String()))
	return cells
}

func parseTable(lines []string, start int) (string, int) {
	headers := splitTableRow(lines[start])
	seps := splitTableRow(lines[start+1])
	aligns := make([]string, len(headers))
	for idx := range headers {
		if idx < len(seps) {
			s := strings.TrimSpace(seps[idx])
			left := strings.HasPrefix(s, ":")
			right := strings.HasSuffix(s, ":")
			switch {
			case left && right:
				aligns[idx] = "center"
			case right:
				aligns[idx] = "right"
			case left:
				aligns[idx] = "left"
			}
		}
	}

	var out strings.Builder
	out.WriteString("<div class=\"table-wrap\"><table>\n<thead><tr>")
	for idx, h := range headers {
		out.WriteString("<th")
		if aligns[idx] != "" {
			out.WriteString(fmt.Sprintf(` style="text-align:%s"`, aligns[idx]))
		}
		out.WriteString(">")
		out.WriteString(renderInline(h))
		out.WriteString("</th>")
	}
	out.WriteString("</tr></thead>\n<tbody>\n")

	i := start + 2
	for i < len(lines) {
		trimmed := strings.TrimSpace(lines[i])
		if trimmed == "" || !looksLikeTableRow(lines[i]) {
			break
		}
		cells := splitTableRow(lines[i])
		out.WriteString("<tr>")
		for idx := range headers {
			val := ""
			if idx < len(cells) {
				val = cells[idx]
			}
			out.WriteString("<td")
			if aligns[idx] != "" {
				out.WriteString(fmt.Sprintf(` style="text-align:%s"`, aligns[idx]))
			}
			out.WriteString(">")
			out.WriteString(renderInline(val))
			out.WriteString("</td>")
		}
		out.WriteString("</tr>\n")
		i++
	}
	out.WriteString("</tbody></table></div>\n")
	return out.String(), i
}

func isListItem(line string) bool {
	return reUnordered.MatchString(line) || reOrdered.MatchString(line)
}

func listIndentAndKind(line string) (indent int, ordered bool, content string, ok bool) {
	if m := reOrdered.FindStringSubmatch(line); m != nil {
		return len(m[1]), true, m[3], true
	}
	if m := reUnordered.FindStringSubmatch(line); m != nil {
		return len(m[1]), false, m[2], true
	}
	return 0, false, "", false
}

func parseList(lines []string, start int) (string, int) {
	baseIndent, ordered, _, _ := listIndentAndKind(lines[start])
	tag := "ul"
	if ordered {
		tag = "ol"
	}

	var out strings.Builder
	out.WriteString("<" + tag + ">\n")

	i := start
	for i < len(lines) {
		line := lines[i]
		if strings.TrimSpace(line) == "" {
			if i+1 < len(lines) {
				if nextIndent, nextOrdered, _, ok := listIndentAndKind(lines[i+1]); ok && nextIndent == baseIndent && nextOrdered == ordered {
					i++
					continue
				}
			}
			break
		}

		indent, itemOrdered, content, ok := listIndentAndKind(line)
		if !ok || indent < baseIndent || (indent == baseIndent && itemOrdered != ordered) {
			break
		}
		if indent > baseIndent {
			subHTML, nextIdx := parseList(lines, i)
			// Attach nested list cleanly; if nothing written yet, wrap it
			out.WriteString(subHTML)
			i = nextIdx
			continue
		}

		// Collect continuation lines at higher indent that aren't new list items
		var itemParts []string
		itemParts = append(itemParts, content)
		i++
		for i < len(lines) {
			next := lines[i]
			nextTrim := strings.TrimSpace(next)
			if nextTrim == "" {
				break
			}
			if _, _, _, isItem := listIndentAndKind(next); isItem {
				break
			}
			if reHeading.MatchString(next) || reHR.MatchString(next) || strings.HasPrefix(nextTrim, ">") {
				break
			}
			if _, _, isFence := parseFenceStart(next); isFence {
				break
			}
			// Continuation text
			itemParts = append(itemParts, nextTrim)
			i++
		}

		joined := strings.Join(itemParts, " ")
		isTask := false
		checked := false
		if tm := reTaskPrefix.FindStringSubmatch(joined); tm != nil {
			isTask = true
			checked = tm[1] == "x" || tm[1] == "X"
			joined = tm[2]
		}

		if isTask {
			checkedAttr := ""
			if checked {
				checkedAttr = " checked"
			}
			out.WriteString(fmt.Sprintf(`<li class="task-item"><input type="checkbox" disabled%s /><span>%s</span>`,
				checkedAttr, renderInline(joined)))
		} else {
			out.WriteString("<li>" + renderInline(joined))
		}

		// Check if immediately followed by a nested list
		if i < len(lines) {
			if subIndent, _, _, subOK := listIndentAndKind(lines[i]); subOK && subIndent > baseIndent {
				subHTML, nextIdx := parseList(lines, i)
				out.WriteString("\n" + subHTML)
				i = nextIdx
			}
		}

		out.WriteString("</li>\n")
	}

	out.WriteString("</" + tag + ">\n")
	return out.String(), i
}

func renderInline(text string) string {
	// Protect inline code spans with placeholder tokens before HTML escaping/formatting.
	var codeTokens []string
	protected := reInlineCode.ReplaceAllStringFunc(text, func(match string) string {
		m := reInlineCode.FindStringSubmatch(match)
		idx := len(codeTokens)
		codeTokens = append(codeTokens, "<code>"+html.EscapeString(strings.TrimSpace(m[2]))+"</code>")
		return fmt.Sprintf("\x00CODE%d\x00", idx)
	})

	// Protect images and links so underscores/asterisks in URLs are not formatted.
	var linkTokens []string
	protected = reImage.ReplaceAllStringFunc(protected, func(match string) string {
		m := reImage.FindStringSubmatch(match)
		idx := len(linkTokens)
		titleAttr := ""
		if m[3] != "" {
			titleAttr = fmt.Sprintf(` title="%s"`, html.EscapeString(m[3]))
		}
		linkTokens = append(linkTokens, fmt.Sprintf(`<img src="%s" alt="%s"%s />`,
			html.EscapeString(m[2]), html.EscapeString(m[1]), titleAttr))
		return fmt.Sprintf("\x00LINK%d\x00", idx)
	})
	protected = reLink.ReplaceAllStringFunc(protected, func(match string) string {
		m := reLink.FindStringSubmatch(match)
		idx := len(linkTokens)
		href := rewriteDocHref(m[2])
		titleAttr := ""
		if m[3] != "" {
			titleAttr = fmt.Sprintf(` title="%s"`, html.EscapeString(m[3]))
		}
		labelHTML := formatEmphasis(html.EscapeString(m[1]))
		linkTokens = append(linkTokens, fmt.Sprintf(`<a href="%s"%s>%s</a>`,
			html.EscapeString(href), titleAttr, labelHTML))
		return fmt.Sprintf("\x00LINK%d\x00", idx)
	})

	escaped := html.EscapeString(protected)
	formatted := formatEmphasis(escaped)

	for idx, tok := range linkTokens {
		formatted = strings.ReplaceAll(formatted, fmt.Sprintf("\x00LINK%d\x00", idx), tok)
	}
	for idx, tok := range codeTokens {
		formatted = strings.ReplaceAll(formatted, fmt.Sprintf("\x00CODE%d\x00", idx), tok)
	}
	return formatted
}

type delimNode struct {
	ch       byte
	count    int
	canOpen  bool
	canClose bool
	outIdx   int
}

func formatEmphasis(s string) string {
	s = reStrike.ReplaceAllString(s, "<del>$1</del>")
	s = resolveEmphasisRuns(s, '*')
	s = resolveEmphasisRuns(s, '_')
	return s
}

func resolveEmphasisRuns(s string, marker byte) string {
	// Collect delimiter runs and split plain segments into tokens so we only
	// emit <strong>/<em> tags when an opener has a matching closer.
	type segment struct {
		text  string
		delim *delimNode
	}
	var segs []segment
	var openStack []*delimNode

	i := 0
	plainStart := 0
	for i < len(s) {
		if s[i] != marker {
			i++
			continue
		}
		if i > plainStart {
			segs = append(segs, segment{text: s[plainStart:i]})
		}
		runStart := i
		for i < len(s) && s[i] == marker {
			i++
		}
		runLen := i - runStart
		plainStart = i

		hasPrev := runStart > 0
		hasNext := i < len(s)
		var prevByte, nextByte byte
		if hasPrev {
			prevByte = s[runStart-1]
		}
		if hasNext {
			nextByte = s[i]
		}

		leftFlank := hasNext && !isASCIIWhitespace(nextByte)
		rightFlank := hasPrev && !isASCIIWhitespace(prevByte)
		canOpen := leftFlank
		canClose := rightFlank
		if marker == '_' {
			prevAlnum := isASCIIAlnum(prevByte)
			nextAlnum := isASCIIAlnum(nextByte)
			canOpen = leftFlank && (!rightFlank || !prevAlnum)
			canClose = rightFlank && (!leftFlank || !nextAlnum)
		}

		// Close any matching openers on stack while remaining > 0
		remaining := runLen
		if canClose {
			for remaining > 0 && len(openStack) > 0 {
				top := openStack[len(openStack)-1]
				if top.count == 0 {
					openStack = openStack[:len(openStack)-1]
					continue
				}
				use := 1
				if remaining >= 2 && top.count >= 2 {
					use = 2
				}
				openTag, closeTag := "<em>", "</em>"
				if use == 2 {
					openTag, closeTag = "<strong>", "</strong>"
				}
				segs[top.outIdx].text = openTag + segs[top.outIdx].text
				segs = append(segs, segment{text: closeTag})
				top.count -= use
				remaining -= use
				if top.count == 0 {
					openStack = openStack[:len(openStack)-1]
				}
			}
		}

		if remaining > 0 {
			node := &delimNode{
				ch:       marker,
				count:    remaining,
				canOpen:  canOpen,
				canClose: canClose,
				outIdx:   len(segs),
			}
			segs = append(segs, segment{text: strings.Repeat(string(marker), remaining), delim: node})
			if canOpen {
				openStack = append(openStack, node)
			}
		}
	}
	if plainStart < len(s) {
		segs = append(segs, segment{text: s[plainStart:]})
	}

	var out strings.Builder
	for _, seg := range segs {
		if seg.delim != nil {
			// Only keep unconsumed delimiter characters
			if seg.delim.count > 0 {
				// seg.text may have had openTags prepended before the original markers
				markerTailIdx := strings.IndexByte(seg.text, marker)
				if markerTailIdx >= 0 {
					out.WriteString(seg.text[:markerTailIdx])
				}
				out.WriteString(strings.Repeat(string(marker), seg.delim.count))
			} else {
				// All markers were consumed as openTags; keep any prepended openTags
				markerTailIdx := strings.IndexByte(seg.text, marker)
				if markerTailIdx >= 0 {
					out.WriteString(seg.text[:markerTailIdx])
				}
			}
			continue
		}
		out.WriteString(seg.text)
	}
	return out.String()
}

func isASCIIWhitespace(b byte) bool {
	return b == ' ' || b == '\t' || b == '\n' || b == '\r'
}

func isASCIIAlnum(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') || (b >= '0' && b <= '9')
}

func rewriteDocHref(href string) string {
	if strings.HasPrefix(href, "http://") || strings.HasPrefix(href, "https://") || strings.HasPrefix(href, "#") || strings.HasPrefix(href, "mailto:") {
		return href
	}
	parts := strings.SplitN(href, "#", 2)
	base := filepath.Base(parts[0])
	if target, ok := docLinkMap[base]; ok {
		if len(parts) == 2 {
			return target + "#" + parts[1]
		}
		return target
	}
	return href
}

func stripInlineFormatting(text string) string {
	s := reLink.ReplaceAllString(text, "$1")
	s = reInlineCode.ReplaceAllString(s, "$2")
	s = formatEmphasis(s)
	s = strings.NewReplacer(
		"<strong>", "", "</strong>", "",
		"<em>", "", "</em>", "",
		"<del>", "", "</del>", "",
	).Replace(s)
	return strings.TrimSpace(s)
}

func uniqueSlug(title string, counts map[string]int) string {
	var b strings.Builder
	lastHyphen := false
	for _, r := range strings.ToLower(title) {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			b.WriteRune(r)
			lastHyphen = false
		case r == ' ' || r == '-' || r == '_':
			if b.Len() > 0 && !lastHyphen {
				b.WriteByte('-')
				lastHyphen = true
			}
		}
	}
	slug := strings.Trim(b.String(), "-")
	if slug == "" {
		slug = "section"
	}
	if n, exists := counts[slug]; exists {
		counts[slug] = n + 1
		return fmt.Sprintf("%s-%d", slug, n)
	}
	counts[slug] = 1
	return slug
}
