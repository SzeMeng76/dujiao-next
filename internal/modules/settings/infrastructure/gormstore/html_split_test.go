package settingsstore

import (
	"strings"
	"testing"
)

// countTag returns how many times an open tag and its matching close tag
// appear in s. Used to assert a chunk never contains a dangling open/close.
func countTag(s, tag string) (open, close int) {
	open = strings.Count(s, "<"+tag)
	close = strings.Count(s, "</"+tag+">")
	return
}

func TestSplitHTMLSafe_ReturnsWholeTextWhenUnderTarget(t *testing.T) {
	text := "<p>短文本</p>"
	got := splitHTMLSafe(text, 300)
	if len(got) != 1 || got[0] != text {
		t.Fatalf("expected single unchanged chunk, got %#v", got)
	}
}

func TestSplitHTMLSafe_KeepsInlineTagsBalanced(t *testing.T) {
	// A <strong> span that crosses where a naive sentence-boundary cut would
	// land must never be torn in half across chunks.
	text := "<p>" + strings.Repeat("这是一段很长的商品描述文字用来测试切片。", 40) +
		"<strong>这句话被加粗了，而且很长，包含标点。还没结束继续加粗的内容一直到这里才结束。</strong>剩余的普通文字。</p>"

	chunks := splitHTMLSafe(text, 300)
	if len(chunks) < 2 {
		t.Fatalf("expected text to be split into multiple chunks, got %d", len(chunks))
	}

	for i, c := range chunks {
		if openN, closeN := countTag(c, "strong"); openN != closeN {
			t.Errorf("chunk %d has unbalanced <strong>: open=%d close=%d: %s", i, openN, closeN, c)
		}
		if openN, closeN := countTag(c, "p"); openN != closeN {
			t.Errorf("chunk %d has unbalanced <p>: open=%d close=%d: %s", i, openN, closeN, c)
		}
	}
}

func TestSplitHTMLSafe_KeepsTableStructureIntact(t *testing.T) {
	// Tiptap wraps table cell content in <p>, so a naive "</p> is always a
	// safe break" rule slices straight through <table>/<tr>/<td>.
	var sb strings.Builder
	sb.WriteString("<table><tbody>")
	for i := 0; i < 30; i++ {
		sb.WriteString("<tr><td><p>规格说明文字，包含一些描述内容用于撑长表格总长度以触发切片逻辑。</p></td><td><p>价格若干元</p></td></tr>")
	}
	sb.WriteString("</tbody></table>")
	text := sb.String()

	chunks := splitHTMLSafe(text, 300)
	if len(chunks) < 2 {
		t.Fatalf("expected table to be split into multiple chunks, got %d", len(chunks))
	}

	for i, c := range chunks {
		for _, tag := range []string{"table", "tbody", "tr", "td", "p"} {
			if openN, closeN := countTag(c, tag); openN != closeN {
				t.Errorf("chunk %d has unbalanced <%s>: open=%d close=%d: %s", i, tag, openN, closeN, c)
			}
		}
		if !strings.HasPrefix(c, "<table>") || !strings.HasSuffix(c, "</table>") {
			t.Errorf("chunk %d is not self-contained <table>...</table>: %s", i, c)
		}
	}
}

func TestSplitHTMLSafe_MixedBlocksNeverSplitMidNode(t *testing.T) {
	var sb strings.Builder
	for i := 0; i < 20; i++ {
		sb.WriteString("<p>段落内容，包含一些说明文字用来撑长内容长度以触发切片逻辑，测试是否会破坏标签结构。</p>")
	}
	sb.WriteString("<ul>")
	for i := 0; i < 20; i++ {
		sb.WriteString("<li>列表项一些描述文字</li>")
	}
	sb.WriteString("</ul>")
	text := sb.String()

	chunks := splitHTMLSafe(text, 300)
	if len(chunks) < 2 {
		t.Fatalf("expected content to be split into multiple chunks, got %d", len(chunks))
	}

	for i, c := range chunks {
		for _, tag := range []string{"p", "ul", "li"} {
			if openN, closeN := countTag(c, tag); openN != closeN {
				t.Errorf("chunk %d has unbalanced <%s>: open=%d close=%d: %s", i, tag, openN, closeN, c)
			}
		}
	}
}

func TestSplitHTMLSafe_PlainTextFallback(t *testing.T) {
	text := strings.Repeat("这是一段没有任何标签的纯文本内容。", 60)
	chunks := splitHTMLSafe(text, 300)
	if len(chunks) < 2 {
		t.Fatalf("expected plain text to be split, got %d chunk(s)", len(chunks))
	}
	if strings.Join(chunks, "") != text {
		t.Errorf("joined plain-text chunks must exactly reconstruct the original")
	}
}
