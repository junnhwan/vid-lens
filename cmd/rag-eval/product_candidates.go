package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
	"vid-lens/internal/eval"
	"vid-lens/internal/repository"
)

type exportedProductCandidate struct {
	Candidate repository.FeedbackCandidate `json:"candidate"`
	SHA256    string                       `json:"candidate_sha256"`
}

func writeCandidateFile(path string, value any) error {
	f, e := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if e != nil {
		return e
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	return enc.Encode(value)
}
func runProductCandidatesCommand(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return errors.New("product-candidates requires export or accept")
	}
	flags := flag.NewFlagSet("product-candidates "+args[0], flag.ContinueOnError)
	output := flags.String("output", "", "new private output path (never overwritten)")
	base := flags.String("base-url", "http://127.0.0.1:8080", "authenticated product server")
	tokenEnv := flags.String("token-env", "VIDLENS_EVAL_TOKEN", "bearer token environment variable")
	input := flags.String("input", "", "private exported candidate array")
	review := flags.String("review", "", "independent human review JSON")
	sessionID := flags.Int64("session-id", 0, "new isolated evaluation session")
	resultPath := flags.String("results", "", "optional W0 product report to export failures without feedback")
	datasetPath := flags.String("dataset", "", "original dataset paired with --results")
	if e := flags.Parse(args[1:]); e != nil {
		return e
	}
	if *output == "" {
		return errors.New("output required")
	}
	switch args[0] {
	case "export":
		if *resultPath != "" {
			if *datasetPath == "" {
				return errors.New("dataset required with results")
			}
			b, e := os.ReadFile(*datasetPath)
			if e != nil {
				return e
			}
			digest := sha256.Sum256(b)
			var cases []eval.ProductCase
			if e = json.Unmarshal(b, &cases); e != nil {
				return e
			}
			b, e = os.ReadFile(*resultPath)
			if e != nil {
				return e
			}
			var report struct {
				DatasetSHA256 string               `json:"dataset_sha256"`
				Results       []eval.ProductResult `json:"results"`
				Identity      json.RawMessage      `json:"identity"`
			}
			if e = json.Unmarshal(b, &report); e != nil {
				return e
			}
			if report.DatasetSHA256 != hex.EncodeToString(digest[:]) {
				return errors.New("result dataset digest mismatch")
			}
			candidates, e := eval.ProductFailureCandidates(cases, report.Results, report.Identity)
			if e != nil {
				return e
			}
			all := []exportedProductCandidate{}
			for _, c := range candidates {
				all = append(all, exportedProductCandidate{Candidate: c, SHA256: eval.CandidateDigest(c)})
			}
			return writeCandidateFile(*output, all)
		}
		token := os.Getenv(*tokenEnv)
		if token == "" {
			return errors.New("token environment variable is empty")
		}
		client := &http.Client{Timeout: 30 * time.Second}
		all := []exportedProductCandidate{}
		seen := map[string]bool{}
		for page := 1; ; page++ {
			req, e := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(*base, "/")+fmt.Sprintf("/api/v1/chat/feedback/candidates?page=%d&page_size=100", page), nil)
			if e != nil {
				return e
			}
			req.Header.Set("Authorization", "Bearer "+token)
			resp, e := client.Do(req)
			if e != nil {
				return e
			}
			var envelope struct {
				Code int `json:"code"`
				Data struct {
					List  []repository.FeedbackCandidate `json:"list"`
					Total int64                          `json:"total"`
				} `json:"data"`
			}
			e = json.NewDecoder(io.LimitReader(resp.Body, 16<<20)).Decode(&envelope)
			resp.Body.Close()
			if e != nil {
				return e
			}
			if resp.StatusCode != 200 || envelope.Code != 200 {
				return errors.New("candidate export denied or failed")
			}
			for _, c := range envelope.Data.List {
				if !seen[c.CandidateID] {
					all = append(all, exportedProductCandidate{Candidate: c, SHA256: eval.CandidateDigest(c)})
					seen[c.CandidateID] = true
				}
			}
			if int64(page*100) >= envelope.Data.Total || len(envelope.Data.List) == 0 {
				break
			}
		}
		return writeCandidateFile(*output, all)
	case "accept":
		if *input == "" || *review == "" {
			return errors.New("input and independent review required")
		}
		b, e := os.ReadFile(*input)
		if e != nil {
			return e
		}
		var candidates []exportedProductCandidate
		if e = json.Unmarshal(b, &candidates); e != nil {
			return e
		}
		b, e = os.ReadFile(*review)
		if e != nil {
			return e
		}
		var r eval.CandidateReview
		d := json.NewDecoder(strings.NewReader(string(b)))
		d.DisallowUnknownFields()
		if e = d.Decode(&r); e != nil {
			return e
		}
		if e = d.Decode(new(any)); e != io.EOF {
			return errors.New("review contains trailing data")
		}
		for _, c := range candidates {
			if c.Candidate.CandidateID == r.CandidateID {
				if c.SHA256 != eval.CandidateDigest(c.Candidate) {
					return errors.New("candidate export changed")
				}
				accepted, e := eval.AcceptProductCandidate(c.Candidate, r, *sessionID)
				if e != nil {
					return e
				}
				return writeCandidateFile(*output, []eval.ProductCase{accepted})
			}
		}
		return errors.New("review candidate not found")
	default:
		return errors.New("unknown product-candidates action")
	}
}
