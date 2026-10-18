package media

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"sync/atomic"
	"syscall"
	"time"
	"vidlens/internal/domain"
)

type progressReader struct {
	r           io.Reader
	last        atomic.Int64
	bytes       int64
	progress    func(int64) error
	reportedAt  time.Time
	progressErr error
}

type downloadProgressKey struct{}
type downloadProgress func(context.Context, string, int64) error

func (s *Service) sourceDownloadContext(ctx context.Context, c domain.Claim) context.Context {
	return context.WithValue(ctx, downloadProgressKey{}, downloadProgress(func(ctx context.Context, transfer string, bytes int64) error {
		return s.Store.SourceDownloadProgress(ctx, c, transfer, bytes)
	}))
}

func newProgressReader(ctx context.Context, r io.Reader, transfer string) *progressReader {
	p := &progressReader{r: r}
	p.last.Store(time.Now().UnixNano())
	if report, ok := ctx.Value(downloadProgressKey{}).(downloadProgress); ok {
		p.progress = func(bytes int64) error { return report(ctx, transfer, bytes) }
	}
	return p
}

func (p *progressReader) Read(b []byte) (int, error) {
	n, e := p.r.Read(b)
	if n > 0 {
		now := time.Now()
		p.last.Store(now.UnixNano())
		p.bytes += int64(n)
		if p.progress != nil && (p.reportedAt.IsZero() || now.Sub(p.reportedAt) >= 30*time.Second) {
			p.reportedAt = now
			if err := p.progress(p.bytes); err != nil {
				p.progressErr = err
				return n, err
			}
		}
	}
	return n, e
}
func (s *Service) downloadStream(ctx context.Context, source, format, dst string, limit int64) error {
	return s.downloadStreamFromInfo(ctx, source, format, dst, limit, "")
}

func (s *Service) downloadStreamFromInfo(ctx context.Context, source, format, dst string, limit int64, infoFile string) error {
	parent := ctx
	if limit <= 0 {
		return domain.Err(422, "INPUT_LIMIT_EXCEEDED", "音视频累计大小超过上限。")
	}
	if _, err := os.Lstat(dst); err == nil {
		return os.ErrExist
	} else if !os.IsNotExist(err) {
		return err
	}
	// Publish only a complete successful transfer. A failed child or skipped
	// fragment cannot leave bytes at the destination used by source validation.
	temporary := dst + ".download-" + domain.ID()
	defer os.Remove(temporary)
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	args := append(s.ytdlpArgs(source), "--format", format, "--output", "-")
	if infoFile != "" {
		args = append(args, "--load-info-json", infoFile)
	} else {
		args = append(args, source)
	}
	cmd := exec.CommandContext(ctx, "yt-dlp", args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		if cmd.Process != nil {
			return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		}
		return nil
	}
	cmd.WaitDelay = 5 * time.Second
	stderr := &boundedBuffer{max: 4096}
	cmd.Stderr = stderr
	pipe, e := cmd.StdoutPipe()
	if e != nil {
		return e
	}
	if e = cmd.Start(); e != nil {
		return errors.New("yt-dlp executable unavailable")
	}
	pr := newProgressReader(ctx, pipe, dst)
	type copied struct {
		bytes int64
		err   error
	}
	done := make(chan copied, 1)
	go func() { _, n, e := copyBounded(ctx, temporary, pr, limit); done <- copied{n, e} }()
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	var failure error
	ctxDone := ctx.Done()
	for {
		select {
		case result := <-done:
			if result.err != nil {
				cancel()
			}
			wait := cmd.Wait()
			if failure != nil {
				return failure
			}
			if result.err != nil {
				if parent.Err() != nil {
					return parent.Err()
				}
				return result.err
			}
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if wait != nil {
				return mediaCommandFailure("yt-dlp", stderr.Bytes(), "未能下载这个视频来源。")
			}
			if result.bytes == 0 {
				return domain.Err(422, "SOURCE_UNAVAILABLE", "来源没有返回可用的媒体字节。")
			}
			// Link is an atomic no-replace operation on this same filesystem.
			// Preserve a destination created concurrently by another operation.
			return os.Link(temporary, dst)
		case <-ticker.C:
			if time.Since(time.Unix(0, pr.last.Load())) > 60*time.Second {
				failure = &domain.WorkError{Code: "SOURCE_UNAVAILABLE", Message: "下载长时间没有字节进展。", Transient: true}
				cancel()
			}
		case <-ctxDone:
			ctxDone = nil
			cancel()
			pipe.Close()
		}
	}
}
