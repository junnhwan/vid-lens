// Package usertags defines deterministic user vocabulary name semantics.
package usertags

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/cases"
	"golang.org/x/text/unicode/norm"
)

type Limits struct{ NameRunes, Aliases, FilterIDs, ListPage, Candidates, Assignments int }

func DefaultLimits() Limits { return Limits{80, 20, 50, 200, 5, 20} }
func (l Limits) WithDefaults() Limits {
	d := DefaultLimits()
	if l.NameRunes <= 0 {
		l.NameRunes = d.NameRunes
	}
	if l.Aliases <= 0 {
		l.Aliases = d.Aliases
	}
	if l.FilterIDs <= 0 {
		l.FilterIDs = d.FilterIDs
	}
	if l.ListPage <= 0 {
		l.ListPage = d.ListPage
	}
	if l.Candidates <= 0 {
		l.Candidates = d.Candidates
	}
	if l.Assignments <= 0 {
		l.Assignments = d.Assignments
	}
	return l
}

func Normalize(name string, limit int) (display, key string, err error) {
	if !utf8.ValidString(name) {
		return "", "", fmt.Errorf("invalid UTF-8 tag name")
	}
	for _, r := range name {
		if unicode.IsControl(r) && !unicode.IsSpace(r) {
			return "", "", fmt.Errorf("tag name contains controls")
		}
	}
	display = strings.Join(strings.Fields(norm.NFKC.String(name)), " ")
	key = cases.Fold().String(display)
	if limit <= 0 {
		limit = 80
	}
	if key == "" || utf8.RuneCountInString(key) > limit || utf8.RuneCountInString(display) > limit {
		return "", "", fmt.Errorf("tag name length outside limits")
	}
	return display, key, nil
}
