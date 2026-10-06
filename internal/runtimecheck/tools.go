// Package runtimecheck reports bounded, local dependency checks. It never
// contacts providers, downloads weights or invokes a model's inference path.
package runtimecheck

import (
	"context"
	"encoding/json"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"time"
	"vid-lens/internal/config"
	"vid-lens/internal/pkg/ffmpeg"
)

type State struct {
	Available  bool      `json:"available"`
	Health     string    `json:"health"`
	ReasonCode string    `json:"reason_code,omitempty"`
	CheckedAt  time.Time `json:"checked_at"`
	Version    string    `json:"version,omitempty"`
}
type Inspector struct {
	tools  config.ToolsConfig
	mu     sync.Mutex
	at     time.Time
	states map[string]State
}

func New(tools config.ToolsConfig) *Inspector { return &Inspector{tools: tools} }
func (i *Inspector) Snapshot(ctx context.Context) map[string]State {
	i.mu.Lock()
	defer i.mu.Unlock()
	if time.Since(i.at) < time.Minute && i.states != nil {
		return clone(i.states)
	}
	// One process check per cache generation, with a global upper bound.
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	states := map[string]State{}
	var mu sync.Mutex
	var wg sync.WaitGroup
	checks := map[string]func() State{
		"ffmpeg": func() State {
			path := i.tools.FFmpegPath
			if path == "" {
				path = "ffmpeg"
			}
			s := command(ctx, path, "-version")
			if s.Available {
				p := command(ctx, ffmpeg.CompanionFFprobePath(path), "-version")
				if !p.Available {
					s.Available = false
					s.Health = "failed"
					s.ReasonCode = "ffprobe_missing"
				}
			}
			return s
		},
		"ocr": func() State {
			s := command(ctx, i.tools.OCRPath, "--version")
			if !s.Available {
				return s
			}
			out, err := exec.CommandContext(ctx, i.tools.OCRPath, "--list-langs").Output()
			if err != nil {
				return failure("ocr_languages_unavailable")
			}
			available := map[string]bool{}
			for _, lang := range strings.Fields(string(out)) {
				available[lang] = true
			}
			for _, lang := range strings.Split(i.tools.OCRLang, "+") {
				if lang != "" && !available[lang] {
					return failure("ocr_language_missing")
				}
			}
			return s
		},
		"url_import": func() State { return command(ctx, i.tools.YtDlpPath, "--version") },
		"alignment": func() State {
			args := i.tools.TranscriptAlignerCommand
			if len(args) == 0 {
				return failure("deployment_disabled")
			}
			if _, err := exec.LookPath(args[0]); err != nil {
				return failure("dependency_missing")
			}
			out, err := exec.CommandContext(ctx, args[0], append(append([]string{}, args[1:]...), "--check")...).Output()
			var r struct {
				Available bool   `json:"available"`
				Health    string `json:"health"`
				Revision  string `json:"revision"`
				Reason    string `json:"error_code"`
			}
			if json.Unmarshal(out, &r) != nil {
				return failure("selfcheck_failed")
			}
			if err != nil || !r.Available {
				switch r.Reason {
				case "dependencies_missing", "model_missing", "model_manifest_missing", "model_manifest_stale", "model_revision_required", "runtime_version_mismatch":
					return failure(r.Reason)
				default:
					return failure("selfcheck_failed")
				}
			}
			if r.Health != "selfcheck_ok" || !regexp.MustCompile(`^[a-f0-9]{40,64}$`).MatchString(r.Revision) {
				return failure("selfcheck_failed")
			}
			return State{Available: true, Health: "selfcheck_ok", Version: r.Revision, CheckedAt: time.Now().UTC()}
		},
	}
	for key, check := range checks {
		wg.Add(1)
		go func(key string, check func() State) {
			defer wg.Done()
			s := check()
			mu.Lock()
			states[key] = s
			mu.Unlock()
		}(key, check)
	}
	wg.Wait()
	if ctx.Err() == nil {
		i.states = states
		i.at = time.Now()
	}
	return states
}
func clone(s map[string]State) map[string]State {
	r := map[string]State{}
	for k, v := range s {
		r[k] = v
	}
	return r
}
func failure(reason string) State {
	return State{Health: "failed", ReasonCode: reason, CheckedAt: time.Now().UTC()}
}
func command(ctx context.Context, path string, args ...string) State {
	if strings.TrimSpace(path) == "" {
		return failure("deployment_disabled")
	}
	if _, err := exec.LookPath(path); err != nil {
		return failure("dependency_missing")
	}
	out, err := exec.CommandContext(ctx, path, args...).Output()
	if err != nil {
		return failure("selfcheck_failed")
	}
	// Raw version output may contain machine paths; only a numeric version escapes.
	version := ""
	for _, part := range strings.Fields(string(out)) {
		if regexp.MustCompile(`^[0-9]+(?:\.[0-9]+){0,4}(?:[-a-zA-Z0-9]+)?$`).MatchString(part) {
			version = strings.Trim(part, "(),")
			break
		}
	}
	return State{Available: true, Health: "selfcheck_ok", Version: version, CheckedAt: time.Now().UTC()}
}
