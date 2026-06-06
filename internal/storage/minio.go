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

// MinIOStorage MinIO 对象存储操作封装
type MinIOStorage struct {
	client   *minio.Client
	bucket   string
	endpoint string
}

// NewMinIOStorage 创建 MinIO 存储实例
func NewMinIOStorage(endpoint, accessKey, secretKey, bucket string, useSSL bool) (*MinIOStorage, error) {
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

	if err := s.ensureBucket(context.Background()); err != nil {
		return nil, err
	}

	return s, nil
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

// ComposeObject 合并分片并返回合并后对象大小
func (s *MinIOStorage) ComposeObject(ctx context.Context, dst string, sources []minio.CopySrcOptions) (int64, error) {
	dstOpts := minio.CopyDestOptions{Bucket: s.bucket, Object: dst}
	info, err := s.client.ComposeObject(ctx, dstOpts, sources...)
	if err != nil {
		return 0, err
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
