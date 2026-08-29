package service

import (
	"context"
	"fmt"
	"strings"
	"unicode"
)

func NormalizeRetrievalQuery(query string) string {
	query = strings.TrimSpace(query)
	var b strings.Builder
	space := false
	for _, r := range query {
		if unicode.IsSpace(r) {
			space = b.Len() > 0
			continue
		}
		if space {
			b.WriteByte(' ')
			space = false
		}
		switch r {
		case '？':
			r = '?'
		case '，':
			r = ','
		case '：':
			r = ':'
		case '；':
			r = ';'
		}
		b.WriteRune(r)
	}
	return strings.TrimSpace(b.String())
}

type PreprocessQueryRewriter struct{}

func (PreprocessQueryRewriter) Rewrite(_ context.Context, input RewriteInput) (RewriteResult, error) {
	query := NormalizeRetrievalQuery(input.Question)
	if query == "" {
		return RewriteResult{}, fmt.Errorf("问题不能为空")
	}
	return RewriteResult{Original: strings.TrimSpace(input.Question), Queries: []string{query}}, nil
}
