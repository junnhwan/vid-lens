// Package summaryselection defines the authoritative selectable text contract.
package summaryselection

import (
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"
	"html"
	"strings"
)

// Text projects Markdown's visible textual content. Links keep labels, images
// keep alt text, code keeps its literal content, and raw HTML is omitted.
// Offsets into this result are Unicode code points, never UTF-16 units.
func Text(markdown string) string {
	source := []byte(strings.ReplaceAll(markdown, "\r\n", "\n"))
	root := goldmark.DefaultParser().Parse(text.NewReader(source))
	var out strings.Builder
	newline := func() {
		if out.Len() > 0 && !strings.HasSuffix(out.String(), "\n") {
			out.WriteByte('\n')
		}
	}
	_ = ast.Walk(root, func(n ast.Node, enter bool) (ast.WalkStatus, error) {
		if enter {
			switch node := n.(type) {
			case *ast.HTMLBlock, *ast.RawHTML:
				return ast.WalkSkipChildren, nil
			case *ast.Text:
				value := node.Segment.Value(source)
				if node.IsRaw() {
					out.Write(value)
				} else {
					out.WriteString(html.UnescapeString(string(util.UnescapePunctuations(value))))
				}
				if node.SoftLineBreak() || node.HardLineBreak() {
					out.WriteByte('\n')
				}
			case *ast.String:
				if node.IsRaw() {
					out.Write(node.Value)
				} else {
					out.WriteString(html.UnescapeString(string(util.UnescapePunctuations(node.Value))))
				}
			case *ast.FencedCodeBlock, *ast.CodeBlock:
				for i := 0; i < n.Lines().Len(); i++ {
					line := n.Lines().At(i)
					out.Write(line.Value(source))
				}
				return ast.WalkSkipChildren, nil
			case *ast.AutoLink:
				out.Write(node.Label(source))
				return ast.WalkSkipChildren, nil
			}
		} else if n.Type() == ast.TypeBlock {
			newline()
		}
		return ast.WalkContinue, nil
	})
	return strings.TrimSpace(out.String())
}
