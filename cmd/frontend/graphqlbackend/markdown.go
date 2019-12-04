package graphqlbackend

import "github.com/sourcegraph/sourcegraph/internal/markdown"

type MarkdownResolver interface {
	Text() string
	HTML() string
	ToMarkdown() (MarkdownResolver, bool)
	ToNoLSIFData() (NoLSIFDataResolver, bool)
}

type markdownResolver struct {
	text string
}

var _ MarkdownResolver = &markdownResolver{}

func NewMarkdownResolver(text string) MarkdownResolver {
	return &markdownResolver{
		text: text,
	}
}

func (m *markdownResolver) Text() string {
	return m.text
}

func (m *markdownResolver) HTML() string {
	return markdown.Render(m.text)
}

func (m *markdownResolver) ToMarkdown() (MarkdownResolver, bool) {
	return m, true
}

func (m *markdownResolver) ToNoLSIFData() (NoLSIFDataResolver, bool) {
	return nil, false
}
