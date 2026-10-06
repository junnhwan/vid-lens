package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"html/template"
	"io"
	"os"
	"strings"
	"vid-lens/internal/eval"
)

func runTranscriptCommand(args []string) error {
	flags := flag.NewFlagSet("transcript", flag.ContinueOnError)
	input := flags.String("dataset", "", "dev transcript windows and independent annotations")
	identityPath := flags.String("identity", "", "frozen code/patch/model/config/version identity JSON (no credentials)")
	output := flags.String("output", "", "new private JSON report, never overwritten")
	review := flags.String("review-output", "", "optional new HTML audit page linking to authorized video playback")
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if *input == "" || *identityPath == "" || *output == "" || flags.NArg() != 0 {
		return fmt.Errorf("transcript requires --dataset, --identity, --output; optional --review-output; offline read-only, no model calls")
	}
	b, err := os.ReadFile(*input)
	if err != nil {
		return err
	}
	var dataset eval.TranscriptDataset
	d := json.NewDecoder(strings.NewReader(string(b)))
	d.DisallowUnknownFields()
	if err := d.Decode(&dataset); err != nil {
		return err
	}
	if err := d.Decode(new(any)); err != io.EOF {
		return fmt.Errorf("dataset must contain exactly one JSON value")
	}
	identityBytes, err := os.ReadFile(*identityPath)
	if err != nil {
		return err
	}
	identity := map[string]string{}
	if err := json.Unmarshal(identityBytes, &identity); err != nil {
		return err
	}
	for _, key := range []string{"code_commit", "patch_sha256", "asr_model", "alignment_model", "assembler_version", "source_mapping_version", "index_version", "config_sha256", "hardware", "preprocessing_version", "annotation_sha256"} {
		if strings.TrimSpace(identity[key]) == "" {
			return fmt.Errorf("identity missing %s (use unverified/none when appropriate)", key)
		}
	}
	for key := range identity {
		if strings.Contains(strings.ToLower(key), "key") || strings.Contains(strings.ToLower(key), "secret") || strings.Contains(strings.ToLower(key), "password") || strings.Contains(strings.ToLower(key), "token") {
			return fmt.Errorf("identity must not contain credentials")
		}
	}
	report, err := eval.EvaluateTranscript(dataset)
	if err != nil {
		return err
	}
	hash := sha256.Sum256(b)
	report.DatasetSHA256 = hex.EncodeToString(hash[:])
	report.Identity = identity
	if *review != "" {
		f, err := os.OpenFile(*review, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err != nil {
			return err
		}
		t, err := template.New("review").Parse(transcriptReviewHTML)
		if err == nil {
			err = t.Execute(f, dataset)
		}
		closeErr := f.Close()
		if err != nil {
			return err
		}
		if closeErr != nil {
			return closeErr
		}
	}
	if err := writeCandidateFile(*output, report); err != nil {
		return err
	}
	if !report.StructurePassed {
		return fmt.Errorf("structural regression failed; inspect %s", *output)
	}
	return nil
}

const transcriptReviewHTML = `<!doctype html><html lang="zh"><meta charset="utf-8"><title>VidLens 转写人工审核</title><style>body{font:16px system-ui;max-width:1000px;margin:40px auto;padding:20px}td,th{padding:12px;text-align:left;border-bottom:1px solid #ddd}p{line-height:1.6}a{color:#1462a3}</style><h1>转写与回放审核</h1><p>结构测试不是人工时间精度。请听原音频，独立填写审核人、文字、允许边界区间和完整回放结论；此页面不会写入真值或发起模型调用。回放链接需要登录原应用。</p>{{range .Cases}}{{$case := .}}<h2>{{.ID}}</h2><p>样本类型：{{.Kind}}；媒体哈希：{{.MediaSHA256}}</p><table><tr><th>句子</th><th>当前定位</th><th>原文与来源</th><th>人工审核</th></tr>{{range .Sentences}}<tr><td>{{.ID}}{{if $case.TaskID}}<br><a href="http://localhost:3000/video/{{$case.TaskID}}?t={{if .StartMS}}{{.StartMS}}{{else}}0{{end}}" target="_blank" rel="noopener">回放当前定位</a>{{end}}</td><td>{{if .StartMS}}{{.StartMS}} ms{{else}}unknown{{end}} — {{if .EndMS}}{{.EndMS}} ms{{end}} · {{.TimeStatus}}</td><td>{{.Text}}<br>{{.SourceIDs}}</td><td>{{if .Annotation}}{{.Annotation.ReviewedBy}}{{else}}未审核{{end}}</td></tr>{{end}}</table>{{if .TaskID}}<p><a href="http://localhost:3000/video/{{.TaskID}}" target="_blank" rel="noopener">打开授权视频，逐句回放与核对</a></p>{{end}}{{end}}</html>`
