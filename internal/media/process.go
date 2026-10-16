package media

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"vidlens/internal/domain"
)

type boundedBuffer struct {
	mu  sync.Mutex
	b   bytes.Buffer
	max int
}

func (b *boundedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	n := len(p)
	if b.b.Len() < b.max {
		_, _ = b.b.Write(p[:min(n, b.max-b.b.Len())])
	}
	return n, nil
}
func (b *boundedBuffer) Bytes() []byte {
	b.mu.Lock()
	defer b.mu.Unlock()
	return bytes.Clone(b.b.Bytes())
}
func command(ctx context.Context, name string, args []string, dir string, quota int64, stall time.Duration) ([]byte, error) {
	return commandLogged(ctx, name, args, dir, quota, stall, nil)
}

// Public failures contain fixed messages only: extractor stderr may contain
// authentication material, local paths, or signed media URLs.
func mediaCommandFailure(name string, stderr []byte, fallback string) error {
	if name == "yt-dlp" {
		message := strings.ToLower(string(stderr))
		if strings.Contains(message, "[youtube") {
			switch {
			case strings.Contains(message, "not a bot"):
				return domain.Err(422, "SOURCE_ACCESS_RESTRICTED", "YouTube 当前要求验证，暂时无法匿名获取这个视频。请稍后重新提交或换一个公开视频。")
			case strings.Contains(message, "private video"), strings.Contains(message, "members-only"), strings.Contains(message, "confirm your age"):
				return domain.Err(422, "SOURCE_ACCESS_RESTRICTED", "这个 YouTube 视频需要登录或有访问限制，当前服务只处理可匿名访问的视频。")
			}
		}
	}
	return domain.Err(422, "SOURCE_UNAVAILABLE", fallback)
}

func commandLogged(ctx context.Context, name string, args []string, dir string, quota int64, stall time.Duration, log *boundedBuffer) ([]byte, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return nil
		}
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
	cmd.WaitDelay = 5 * time.Second
	out := &boundedBuffer{max: 4 << 20}
	stderr := log
	if stderr == nil {
		stderr = &boundedBuffer{max: 4096}
	}
	cmd.Stdout = out
	cmd.Stderr = stderr
	if e := cmd.Start(); e != nil {
		return nil, errors.New("required media executable is unavailable")
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	lastBytes := int64(-1)
	lastChange := time.Now()
	var failure error
	for {
		select {
		case e := <-done:
			if failure != nil {
				return nil, failure
			}
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			if e != nil {
				return nil, mediaCommandFailure(name, stderr.Bytes(), "媒体工具未能处理这个来源。")
			}
			return out.Bytes(), nil
		case <-tick.C:
			if dir == "" {
				continue
			}
			n, e := dirSize(dir)
			if e != nil {
				failure = e
				cancel()
				continue
			}
			if quota > 0 && n > quota {
				failure = domain.Err(422, "INPUT_LIMIT_EXCEEDED", "媒体工作空间超过限制。")
				cancel()
			}
			if n != lastBytes {
				lastBytes = n
				lastChange = time.Now()
			} else if stall > 0 && time.Since(lastChange) > stall {
				failure = domain.Err(503, "SOURCE_UNAVAILABLE", "下载长时间没有进展。")
				cancel()
			}
			if rss := processRSS(cmd.Process.Pid); rss > 512<<20 {
				failure = domain.Err(422, "RESOURCE_LIMIT_EXCEEDED", "媒体子进程超过内存限制。")
				cancel()
			}
		}
	}
}
func processRSS(pid int) int64 { // Linux deployment has an additional cgroup limit; this observes the main child.
	b, e := os.ReadFile(filepath.Join("/proc", stringInt(pid), "statm"))
	if e != nil {
		return 0
	}
	var total, rss int64
	_, _ = fmtSscan(string(b), &total, &rss)
	return rss * int64(os.Getpagesize())
}
func dirSize(dir string) (int64, error) {
	var size int64
	e := filepath.WalkDir(dir, func(path string, d os.DirEntry, e error) error {
		if e != nil {
			return e
		}
		if !d.IsDir() {
			st, e := d.Info()
			if e != nil {
				return e
			}
			size += st.Size()
		}
		return nil
	})
	return size, e
}
func copyBounded(ctx context.Context, dst string, src io.Reader, limit int64) (string, int64, error) {
	f, e := os.OpenFile(dst, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if e != nil {
		return "", 0, e
	}
	defer f.Close()
	h := newHash()
	n, e := io.Copy(io.MultiWriter(f, h), io.LimitReader(&contextReader{ctx: ctx, r: src}, limit+1))
	if e != nil {
		return "", n, e
	}
	if n > limit {
		return "", n, domain.Err(422, "INPUT_LIMIT_EXCEEDED", "下载数据超过大小限制。")
	}
	if e = f.Sync(); e != nil {
		return "", n, e
	}
	return hashString(h), n, nil
}

type contextReader struct {
	ctx context.Context
	r   io.Reader
}

func (r *contextReader) Read(p []byte) (int, error) {
	if e := r.ctx.Err(); e != nil {
		return 0, e
	}
	return r.r.Read(p)
}
