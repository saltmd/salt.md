package server

import (
	"strings"
	"testing"
)

func TestPublicReaderPreservesNativeBlockProperties(t *testing.T) {
	content := []byte(`[
 {"id":"title","type":"heading","props":{"level":2,"textAlignment":"center","textColor":"blue"},"content":[{"type":"text","text":"Title","styles":{"bold":true,"code":true,"textColor":"red","backgroundColor":"yellow"}}]},
 {"type":"image","props":{"url":"/public/token/files/p.png","name":"Image","previewWidth":342,"textAlignment":"right","caption":"A <caption>"}},
 {"type":"image","props":{"url":"/public/token/files/q.png","showPreview":false,"name":"Download image","caption":"Description"}},
 {"type":"callout","props":{"emoji":"⚠️"},"content":[{"type":"text","text":"Careful"}]},
 {"type":"columns","props":{"count":3},"children":[{"type":"paragraph","props":{"backgroundColor":"pink"},"content":[{"type":"text","text":"Column"}]}]},
 {"type":"toggleListItem","content":[{"type":"text","text":"Details"}],"children":[{"type":"paragraph","props":{"backgroundColor":"green"},"content":[{"type":"text","text":"Nested"}]}]},
 {"type":"heading","props":{"level":3,"isToggleable":true},"content":[{"type":"text","text":"Expandable heading"}],"children":[{"type":"paragraph","content":[{"type":"text","text":"Heading child"}]}]},
 {"type":"table","content":{"type":"tableContent","columnWidths":[120,240],"headerRows":1,"rows":[{"cells":[{"type":"tableCell","props":{"backgroundColor":"purple","textColor":"blue"},"content":[{"type":"text","text":"Cell","styles":{"backgroundColor":"orange"}}]}]}]}},
 {"type":"toc"}
 ]`)
	got := publicBlocksHTML(content)
	for _, want := range []string{`id="block-title"`, `data-text-alignment="center"`, `data-text-color="blue"`, `data-text-color="red"`, `data-background-color="yellow"`, `<code><strong>Title</strong></code>`, `width="342"`, `width:min(100%, 342px)`, `data-text-alignment="right"`, `A &lt;caption&gt;`, `class="bn-file-caption"`, `data-tone="warning"`, `data-count="3"`, `data-background-color="pink"`, `data-background-color="green"`, `data-background-color="purple"`, `data-background-color="orange"`, `<col style="width:120px">`, `<th`, `href="#block-title"`, `Download image`} {
		if !strings.Contains(got, want) {
			t.Errorf("missing property %s in %s", want, got)
		}
	}
	if strings.Contains(got, `src="/public/token/files/q.png"`) {
		t.Fatal("showPreview=false ignored")
	}
	if !strings.Contains(got, `<details><summary><h3 class="bn-inline-content">Expandable heading</h3></summary>`) || strings.Count(got, "Heading child") != 1 {
		t.Fatal("toggle heading ignored or duplicated")
	}
	// The export path retains its independent print behaviour.
	if strings.Contains(blocksToHTML(content), `class="public-blocks`) {
		t.Fatal("public changes leaked into print/export")
	}
}

func TestPublicFormattingRejectsMarkupAndStyleInjection(t *testing.T) {
	content := []byte(`[{"id":"\" onmouseover=\"evil()","type":"paragraph","props":{"textAlignment":"left;position:fixed","backgroundColor":"pink\" style=\"position:fixed","textColor":"url(javascript:evil())"},"content":[{"type":"text","text":"<script>evil()</script>","styles":{"backgroundColor":"expression(evil())"}},{"type":"link","href":"javascript:evil()","content":[{"type":"text","text":"Link"}]}]},{"type":"image","props":{"url":"javascript:evil()","previewWidth":-8,"caption":"<img src=x onerror=evil()>"}}]`)
	got := publicBlocksHTML(content)
	for _, bad := range []string{`<script>`, `href="javascript:`, `src="javascript:`, `width="-8"`, `style="position:fixed`, `data-text-color="url(`, `data-background-color="expression(`, `<img src=x`} {
		if strings.Contains(got, bad) {
			t.Fatal("unsafe formatting", bad)
		}
	}
	if !strings.Contains(got, `href="#"`) || !strings.Contains(got, `&lt;script&gt;`) {
		t.Fatal("missing safe escaped content")
	}
}
