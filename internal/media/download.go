package media

import (
	"context"
	"errors"
	"io"
	"os/exec"
	"sync/atomic"
	"syscall"
	"time"
	"vidlens/internal/domain"
)

type progressReader struct {
	r    io.Reader
	last atomic.Int64
}

func (p *progressReader) Read(b []byte) (int, error) {
	n, e := p.r.Read(b)
	if n > 0 {
		p.last.Store(time.Now().UnixNano())
	}
	return n, e
}
func (s *Service) downloadStream(ctx context.Context, source, format, dst string, limit int64) error {
	if limit <= 0 {
		return domain.Err(422, "INPUT_LIMIT_EXCEEDED", "音视频累计大小超过上限。")
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	args := append(s.ytdlpArgs(), "--format", format, "--output", "-", source)
	cmd := exec.CommandContext(ctx, "yt-dlp", args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		if cmd.Process != nil {
			return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		}
		return nil
	}
	cmd.WaitDelay = 5 * time.Second
	cmd.Stderr = &boundedBuffer{max: 4096}
	pipe, e := cmd.StdoutPipe()
	if e != nil {
		return e
	}
	if e = cmd.Start(); e != nil {
		return errors.New("yt-dlp executable unavailable")
	}
	pr := &progressReader{r: pipe}
	pr.last.Store(time.Now().UnixNano())
	done := make(chan error, 1)
	go func() { _, _, e := copyBounded(ctx, dst, pr, limit); done <- e }()
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	var failure error
	for {
		select {
		case e := <-done:
			if e != nil {
				cancel()
			}
			wait := cmd.Wait()
			if failure != nil {
				return failure
			}
			if e != nil {
				return e
			}
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if wait != nil {
				return domain.Err(422, "SOURCE_UNAVAILABLE", "未能下载这个视频来源。")
			}
			return nil
		case <-ticker.C:
			if time.Since(time.Unix(0, pr.last.Load())) > 60*time.Second {
				failure = &domain.WorkError{Code: "SOURCE_UNAVAILABLE", Message: "下载长时间没有字节进展。", Transient: true}
				cancel()
			}
		case <-ctx.Done():
			cancel()
			pipe.Close()
		}
	}
}
