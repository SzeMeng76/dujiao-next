package settingsstore

import (
	"bytes"
	"strings"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

// splitHTMLSafe splits text into chunks of roughly targetSize runes each so a
// long rich-text field can be translated in several independent model calls.
// It parses text as an HTML fragment and only ever cuts between complete
// sibling nodes, never through a tag or entity. A single node too big to fit
// on its own (e.g. a <table> or a very long <p>) is recursed into: its
// children are chunked the same way and every resulting chunk is re-wrapped
// in a clone of that node's own open/close tags, so each chunk stays valid,
// self-contained HTML with every tag paired — a stray unmatched tag in one
// chunk is exactly what previously made the model echo it back literally
// (e.g. a literal "<strong>") instead of preserving real markup.
// If text doesn't parse as HTML at all, it falls back to splitting the raw
// text on sentence/whitespace boundaries, which is always safe since there
// are no tags to break.
func splitHTMLSafe(text string, targetSize int) []string {
	if len([]rune(text)) <= targetSize {
		return []string{text}
	}

	nodes, err := parseHTMLFragment(text)
	if err != nil || len(nodes) == 0 {
		return splitPlainTextSafe(text, targetSize)
	}
	return chunkNodes(nodes, targetSize)
}

// minTagBudget is the floor used when a node's own open/close tags eat into
// its target chunk size (e.g. a <table> with a long class list); we'd rather
// produce slightly-over-budget chunks than a nonsensical zero/negative size.
const minTagBudget = 100

// chunkNodes groups sibling nodes into chunks of at most targetSize runes
// (rendered), splitting only between nodes. See splitHTMLSafe for how an
// oversized single node is handled.
func chunkNodes(nodes []*html.Node, targetSize int) []string {
	var chunks []string
	var buf strings.Builder
	bufLen := 0

	flush := func() {
		if buf.Len() > 0 {
			chunks = append(chunks, buf.String())
			buf.Reset()
			bufLen = 0
		}
	}

	for _, n := range nodes {
		rendered := renderNodes(n)
		renderedLen := len([]rune(rendered))

		if renderedLen > targetSize {
			flush()
			chunks = append(chunks, splitOversizedNode(n, rendered, targetSize)...)
			continue
		}

		if bufLen+renderedLen > targetSize && bufLen > 0 {
			flush()
		}
		buf.WriteString(rendered)
		bufLen += renderedLen
	}
	flush()
	return chunks
}

// splitOversizedNode handles a single node whose rendered HTML alone exceeds
// targetSize.
func splitOversizedNode(n *html.Node, rendered string, targetSize int) []string {
	if n.FirstChild == nil {
		// Leaf node with no children: nothing to recurse into. A text node
		// can still be split on its raw (unescaped) content — there's no
		// markup left to break at this point — then each piece is
		// re-escaped. Any other empty leaf (e.g. a void element with a long
		// attribute list) is left as a single over-budget chunk rather than
		// risk corrupting it.
		if n.Type == html.TextNode {
			parts := splitPlainTextSafe(n.Data, targetSize)
			out := make([]string, len(parts))
			for i, part := range parts {
				out[i] = html.EscapeString(part)
			}
			return out
		}
		return []string{rendered}
	}

	open, closeTag := openCloseTags(n)
	budget := targetSize - len([]rune(open)) - len([]rune(closeTag))
	if budget < minTagBudget {
		budget = minTagBudget
	}

	childGroups := chunkNodes(collectChildren(n), budget)
	out := make([]string, len(childGroups))
	for i, group := range childGroups {
		out[i] = open + group + closeTag
	}
	return out
}

// parseHTMLFragment parses text as the children of a <body> element, so
// fragments like "<p>a</p><p>b</p>" parse as-is without being wrapped in a
// full <html><head><body> document.
func parseHTMLFragment(text string) ([]*html.Node, error) {
	context := &html.Node{Type: html.ElementNode, Data: "body", DataAtom: atom.Body}
	return html.ParseFragment(strings.NewReader(text), context)
}

// renderNodes serializes n (and its subtree) back to HTML. It does not
// touch n's parent or siblings, so it's safe to call on a node that's still
// attached to the tree produced by parseHTMLFragment.
func renderNodes(n *html.Node) string {
	var buf bytes.Buffer
	_ = html.Render(&buf, n)
	return buf.String()
}

// collectChildren reads n's children into a slice without mutating the tree.
func collectChildren(n *html.Node) []*html.Node {
	var children []*html.Node
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		children = append(children, c)
	}
	return children
}

// openCloseTags renders n's own open and close tag (e.g. `<table class="x">`
// and `</table>`) with no children, by rendering a detached clone that
// shares n's tag name and attributes.
func openCloseTags(n *html.Node) (open, closeTag string) {
	clone := &html.Node{Type: n.Type, DataAtom: n.DataAtom, Data: n.Data, Namespace: n.Namespace, Attr: n.Attr}
	rendered := renderNodes(clone)
	tail := "</" + n.Data + ">"
	if strings.HasSuffix(rendered, tail) {
		return strings.TrimSuffix(rendered, tail), tail
	}
	// Void element (e.g. <img>, <br>) — no separate close tag and, by
	// definition, no children, so this path isn't actually reached from
	// splitOversizedNode (which already checked n.FirstChild != nil).
	return rendered, ""
}

// plainTextBreakpoints are tried from most to least preferred when splitting
// plain text (no markup) — paragraph/sentence boundaries first, falling back
// to any whitespace.
var plainTextBreakpoints = []string{"\n\n", "\n", "。", "！", "？", ". ", "! ", "? ", " "}

// splitPlainTextSafe splits text (assumed to contain no HTML markup) into
// chunks of at most targetSize runes, preferring to cut at a
// plainTextBreakpoints boundary closest to the target.
func splitPlainTextSafe(text string, targetSize int) []string {
	runes := []rune(text)
	total := len(runes)
	if total <= targetSize {
		return []string{text}
	}

	var chunks []string
	start := 0
	for start < total {
		end := start + targetSize
		if end >= total {
			chunks = append(chunks, string(runes[start:total]))
			break
		}

		cut := findPlainTextBreak(runes, start, end)
		if cut <= start {
			cut = end
		}
		chunks = append(chunks, string(runes[start:cut]))
		start = cut
	}
	return chunks
}

// findPlainTextBreak searches [searchStart, limit) for the breakpoint marker
// whose end lands closest to limit. Returns -1 if none is found.
func findPlainTextBreak(runes []rune, searchStart, limit int) int {
	window := string(runes[searchStart:limit])
	best := -1
	for _, marker := range plainTextBreakpoints {
		if idx := strings.LastIndex(window, marker); idx >= 0 {
			cut := searchStart + len([]rune(window[:idx])) + len([]rune(marker))
			if cut > best {
				best = cut
			}
		}
	}
	if best <= searchStart {
		return -1
	}
	return best
}
