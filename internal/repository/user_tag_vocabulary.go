package repository

import (
	"context"
	"encoding/json"
	"sort"
	"strings"

	"golang.org/x/text/cases"
	"golang.org/x/text/unicode/norm"
	"gorm.io/gorm"
	"vid-lens/internal/model"
	"vid-lens/internal/processing"
)

// FreezeVocabulary serializes with owner namespace edits and freezes a bounded
// deterministic name/alias shortlist. Match context contains the user request
// and, when already available, the accepted source; no embedding is performed.
// Streaming the owner's namespace keeps memory bounded even for large wordbooks.
func (r *UserTagRepository) FreezeVocabulary(ctx context.Context, owner int64, matchContext string, byteLimits ...int) (*processing.TagVocabularySnapshot, error) {
	byteLimit := processing.MaxTagVocabularyBytes
	if len(byteLimits) > 0 {
		byteLimit = max(128, min(byteLimit, byteLimits[0]))
	}
	out := &processing.TagVocabularySnapshot{Candidates: []processing.TagVocabularyEntry{}}
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := r.lockVocabulary(tx, owner); err != nil {
			return err
		}
		var head model.UserTagVocabularyHead
		if err := tx.Where("user_id = ?", owner).First(&head).Error; err != nil {
			return err
		}
		out.Version = head.Version
		contextKey := cases.Fold().String(strings.Join(strings.Fields(norm.NFKC.String(matchContext)), " "))
		type ranked struct {
			entry processing.TagVocabularyEntry
			score int
			key   string
		}
		selected := []ranked{}
		less := func(a, b ranked) bool {
			if a.score != b.score {
				return a.score > b.score
			}
			if a.key != b.key {
				return a.key < b.key
			}
			return a.entry.TagID < b.entry.TagID
		}
		var current ranked
		flush := func() {
			if current.entry.TagID == "" {
				return
			}
			sort.Strings(current.entry.Aliases)
			selected = append(selected, current)
			sort.Slice(selected, func(i, j int) bool { return less(selected[i], selected[j]) })
			if len(selected) > processing.MaxTagVocabularyCandidates {
				out.Truncated = true
				selected = selected[:processing.MaxTagVocabularyCandidates]
			}
		}
		rows, err := tx.Table("user_tag_names AS n").Select("t.id, t.display_name, n.display_name, n.normalized_key, n.kind").Joins("JOIN user_tags AS t ON t.id = n.tag_id AND t.user_id = n.user_id").Where("n.user_id = ? AND t.status = ?", owner, "active").Order("t.id, n.normalized_key").Rows()
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var id, name, alias, key, kind string
			if err := rows.Scan(&id, &name, &alias, &key, &kind); err != nil {
				return err
			}
			if current.entry.TagID != id {
				flush()
				current = ranked{entry: processing.TagVocabularyEntry{TagID: id, Name: name, Aliases: []string{}}, key: cases.Fold().String(norm.NFKC.String(name))}
			}
			if kind == "alias" && len(current.entry.Aliases) < 20 {
				current.entry.Aliases = append(current.entry.Aliases, alias)
			}
			if strings.Contains(contextKey, key) {
				current.score++
			}
		}
		if err := rows.Err(); err != nil {
			return err
		}
		flush()
		for _, candidate := range selected {
			out.Candidates = append(out.Candidates, candidate.entry)
			raw, err := json.Marshal(out)
			if err != nil {
				return err
			}
			for len(raw) > byteLimit-32 && len(out.Candidates[len(out.Candidates)-1].Aliases) > 0 {
				out.Truncated = true
				entry := &out.Candidates[len(out.Candidates)-1]
				entry.Aliases = entry.Aliases[:len(entry.Aliases)-1]
				raw, err = json.Marshal(out)
				if err != nil {
					return err
				}
			}
			if len(raw) > byteLimit-32 {
				out.Candidates = out.Candidates[:len(out.Candidates)-1]
				out.Truncated = true
				break
			}
		}
		return nil
	})
	return out, err
}
