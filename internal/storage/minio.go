package storage

import (
	"context"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

const minComposePartSize int64 = 5 * 1024 * 1024

// objectPartOpener 按需打开一个待合并的源对象。
type objectPartOpener func(context.Context, minio.CopySrcOptions) (io.ReadCloser, error)

// MinIOStorage MinIO 对象存储操作封装
type MinIOStorage struct {
	client   *minio.Client
	bucket   string
	endpoint string
}

// NewMinIOStorageWithContext 创建并初始化 MinIO 存储实例。
func NewMinIOStorageWithContext(ctx context.Context, endpoint, accessKey, secretKey, bucket string, useSSL bool) (*MinIOStorage, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	client, err := minio.New(endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(accessKey, secretKey, ""),
		Secure: useSSL,
	})
	if err != nil {
		return nil, fmt.Errorf("创建 MinIO 客户端失败: %w", err)
	}

	s := &MinIOStorage{
		client:   client,
		bucket:   bucket,
		endpoint: endpoint,
	}

	if err := s.ensureBucket(ctx); err != nil {
		return nil, err
	}

	return s, nil
}

// HealthCheck verifies that the configured bucket is reachable and exists.
// Unlike ensureBucket, readiness checks must not mutate storage state.
func (s *MinIOStorage) HealthCheck(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	exists, err := s.client.BucketExists(ctx, s.bucket)
	if err != nil {
		return fmt.Errorf("检查 bucket 失败: %w", err)
	}
	if !exists {
		return fmt.Errorf("bucket %q 不存在", s.bucket)
	}
	return nil
}

// ensureBucket 确保 bucket 存在（默认私有权限）
func (s *MinIOStorage) ensureBucket(ctx context.Context) error {
	exists, err := s.client.BucketExists(ctx, s.bucket)
	if err != nil {
		return fmt.Errorf("检查 bucket 失败: %w", err)
	}
	if !exists {
		if err := s.client.MakeBucket(ctx, s.bucket, minio.MakeBucketOptions{}); err != nil {
			return fmt.Errorf("创建 bucket 失败: %w", err)
		}
	}
	return nil
}

// UploadFile 上传文件流
func (s *MinIOStorage) UploadFile(ctx context.Context, objectName string, reader io.Reader, size int64, contentType string) error {
	_, err := s.client.PutObject(ctx, s.bucket, objectName, reader, size, minio.PutObjectOptions{
		ContentType: contentType,
	})
	return err
}

// UploadFromPath 上传本地文件
func (s *MinIOStorage) UploadFromPath(ctx context.Context, localPath, objectName string, contentType string) (int64, error) {
	info, err := s.client.FPutObject(ctx, s.bucket, objectName, localPath, minio.PutObjectOptions{
		ContentType: contentType,
	})
	if err != nil {
		return 0, err
	}
	return info.Size, nil
}

// GetPresignedURL 生成预签名下载 URL（5分钟有效）
func (s *MinIOStorage) GetPresignedURL(ctx context.Context, objectName string) (string, error) {
	reqParams := make(url.Values)
	if ext := filepath.Ext(objectName); ext != "" {
		switch ext {
		case ".mp4":
			reqParams.Set("response-content-type", "video/mp4")
		case ".mp3":
			reqParams.Set("response-content-type", "audio/mpeg")
		case ".wav":
			reqParams.Set("response-content-type", "audio/wav")
		}
	}

	presignedURL, err := s.client.PresignedGetObject(ctx, s.bucket, objectName, 5*time.Minute, reqParams)
	if err != nil {
		return "", fmt.Errorf("生成预签名 URL 失败: %w", err)
	}
	return presignedURL.String(), nil
}

// DeleteObject 删除对象
func (s *MinIOStorage) DeleteObject(ctx context.Context, objectName string) error {
	return s.client.RemoveObject(ctx, s.bucket, objectName, minio.RemoveObjectOptions{})
}

// requiresStreamingMerge 判断源对象是否违反 MinIO ComposeObject 的限制：
// 除最后一个 source 外，每个 source 都必须至少为 5 MiB。
func requiresStreamingMerge(sizes []int64) bool {
	for i := 0; i < len(sizes)-1; i++ {
		if sizes[i] < minComposePartSize {
			return true
		}
	}
	return false
}

// copyObjectParts 按 sources 的顺序逐个读取并写入目标流，避免同时打开全部分片。
func copyObjectParts(ctx context.Context, dst io.Writer, sources []minio.CopySrcOptions, open objectPartOpener) error {
	for _, source := range sources {
		if err := ctx.Err(); err != nil {
			return err
		}

		reader, err := open(ctx, source)
		if err != nil {
			return fmt.Errorf("打开分片对象 %s/%s 失败: %w", source.Bucket, source.Object, err)
		}

		_, copyErr := io.Copy(dst, reader)
		closeErr := reader.Close()
		if copyErr != nil {
			return fmt.Errorf("读取分片对象 %s/%s 失败: %w", source.Bucket, source.Object, copyErr)
		}
		if closeErr != nil {
			return fmt.Errorf("关闭分片对象 %s/%s 失败: %w", source.Bucket, source.Object, closeErr)
		}
	}
	return nil
}

// ComposeObject 合并分片并返回合并后对象大小。
// 大分片继续使用 MinIO 服务端 ComposeObject；若非末尾分片小于 5 MiB，
// 则顺序读取各分片并流式 PutObject，避免把完整文件加载到内存。
func (s *MinIOStorage) ComposeObject(ctx context.Context, dst string, sources []minio.CopySrcOptions) (int64, error) {
	if len(sources) == 0 {
		return 0, fmt.Errorf("合并分片失败: 源对象不能为空")
	}

	sizes := make([]int64, len(sources))
	var totalSize int64
	for i, source := range sources {
		statOpts := minio.StatObjectOptions{
			ServerSideEncryption: source.Encryption,
			VersionID:            source.VersionID,
		}
		info, err := s.client.StatObject(ctx, source.Bucket, source.Object, statOpts)
		if err != nil {
			return 0, fmt.Errorf("检查分片对象 %s/%s 失败: %w", source.Bucket, source.Object, err)
		}

		size := info.Size
		if source.MatchRange {
			if source.Start < 0 || source.End < source.Start || source.End >= info.Size {
				return 0, fmt.Errorf(
					"分片对象 %s/%s 的读取范围 [%d, %d] 无效（对象大小为 %d）",
					source.Bucket, source.Object, source.Start, source.End, info.Size,
				)
			}
			size = source.End - source.Start + 1
		}
		sizes[i] = size
		totalSize += size
	}

	if !requiresStreamingMerge(sizes) {
		dstOpts := minio.CopyDestOptions{Bucket: s.bucket, Object: dst}
		info, err := s.client.ComposeObject(ctx, dstOpts, sources...)
		if err != nil {
			return 0, err
		}
		return info.Size, nil
	}

	pipeReader, pipeWriter := io.Pipe()
	copyResult := make(chan error, 1)
	go func() {
		err := copyObjectParts(ctx, pipeWriter, sources, func(ctx context.Context, source minio.CopySrcOptions) (io.ReadCloser, error) {
			getOpts := minio.GetObjectOptions{
				ServerSideEncryption: source.Encryption,
				VersionID:            source.VersionID,
			}
			if source.MatchRange {
				if err := getOpts.SetRange(source.Start, source.End); err != nil {
					return nil, err
				}
			}
			return s.client.GetObject(ctx, source.Bucket, source.Object, getOpts)
		})
		_ = pipeWriter.CloseWithError(err)
		copyResult <- err
	}()

	info, putErr := s.client.PutObject(ctx, s.bucket, dst, pipeReader, totalSize, minio.PutObjectOptions{
		ContentType: "application/octet-stream",
	})
	// PutObject 可能在读取完 pipe 前失败；主动关闭 reader 以解除复制 goroutine 的阻塞。
	_ = pipeReader.CloseWithError(putErr)
	copyErr := <-copyResult
	if putErr != nil {
		return 0, putErr
	}
	if copyErr != nil {
		return 0, copyErr
	}
	return info.Size, nil
}

// ObjectExists 检查对象是否存在
func (s *MinIOStorage) ObjectExists(ctx context.Context, objectName string) (bool, error) {
	_, err := s.client.StatObject(ctx, s.bucket, objectName, minio.StatObjectOptions{})
	if err != nil {
		return false, nil
	}
	return true, nil
}

// DownloadToTemp 将 MinIO 对象下载到本地临时文件，调用方负责删除返回路径。
func (s *MinIOStorage) DownloadToTemp(ctx context.Context, objectName string) (string, error) {
	ext := filepath.Ext(objectName)
	tmp, err := os.CreateTemp("", "vidlens_video_*"+ext)
	if err != nil {
		return "", err
	}
	tmpPath := tmp.Name()
	if err := tmp.Close(); err != nil {
		os.Remove(tmpPath)
		return "", err
	}

	if err := s.client.FGetObject(ctx, s.bucket, objectName, tmpPath, minio.GetObjectOptions{}); err != nil {
		os.Remove(tmpPath)
		return "", err
	}
	return tmpPath, nil
}

// BucketName 返回桶名
func (s *MinIOStorage) BucketName() string {
	return s.bucket
}
