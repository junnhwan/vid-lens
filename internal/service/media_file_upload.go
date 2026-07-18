package service

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"vid-lens/internal/model"
	"vid-lens/internal/repository"

	"github.com/google/uuid"
)

// 普通文件上传、资产创建和文件内容校验。
// UploadFile 普通文件上传
func (s *MediaService) UploadFile(ctx context.Context, userID int64, filename string, fileStream io.Reader, fileSize int64) (*UploadResult, error) {
	if err := s.validateUploadSize(fileSize); err != nil {
		return nil, err
	}

	tmpPath, fileMD5, actualSize, err := copyStreamToTempAndHash(fileStream, s.cfg.MaxFileSize)
	if err != nil {
		return nil, fmt.Errorf("读取文件失败: %w", err)
	}
	defer os.Remove(tmpPath)

	// 内容级去重
	asset, err := s.repo.Asset.FindByMD5(fileMD5)
	if err != nil {
		return nil, err
	}

	ext := filepath.Ext(filename)
	objectName := fmt.Sprintf("videos/%s%s", uuid.New().String(), ext)
	if asset == nil {
		asset, err = s.createAssetFromLocalFile(ctx, fileMD5, tmpPath, objectName, contentTypeForFilename(filename), actualSize)
		if err != nil {
			return nil, err
		}
	}

	return s.createTaskFromAsset(userID, filename, asset, model.TaskStatusPending)
}

func (s *MediaService) createTaskFromAsset(userID int64, filename string, asset *model.VideoAsset, status int8) (*UploadResult, error) {
	var task *model.VideoTask
	var activeAsset *model.VideoAsset
	err := s.repo.Transaction(func(txRepos *repository.Repositories) error {
		lockedAsset, err := txRepos.Asset.FindActiveByIDForUpdate(asset.ID)
		if err != nil {
			return err
		}
		activeAsset = lockedAsset
		task = &model.VideoTask{
			UserID:   userID,
			AssetID:  &lockedAsset.ID,
			FileMD5:  lockedAsset.FileMD5,
			Filename: filename,
			FileURL:  lockedAsset.ObjectName,
			FileSize: lockedAsset.FileSize,
			Status:   status,
			Stage:    model.TaskStageUploaded,
			TraceID:  uuid.New().String(),
		}
		return txRepos.Task.Create(task)
	})
	if err != nil {
		return nil, err
	}

	return &UploadResult{
		TaskID:   task.ID,
		FileMD5:  activeAsset.FileMD5,
		Filename: filename,
		FileURL:  activeAsset.ObjectName,
		FileSize: activeAsset.FileSize,
		Status:   status,
		Stage:    task.Stage,
		TraceID:  task.TraceID,
	}, nil
}

func (s *MediaService) validateUploadSize(fileSize int64) error {
	if s.cfg.MaxFileSize > 0 && fileSize > s.cfg.MaxFileSize {
		return fmt.Errorf("文件大小超过限制: 最大 %d 字节", s.cfg.MaxFileSize)
	}
	return nil
}

func (s *MediaService) createAssetFromLocalFile(ctx context.Context, fileMD5, localPath, objectName, contentType string, size int64) (*model.VideoAsset, error) {
	if _, err := s.storage.UploadFromPath(ctx, localPath, objectName, contentType); err != nil {
		return nil, fmt.Errorf("上传到 MinIO 失败: %w", err)
	}

	asset := &model.VideoAsset{
		FileMD5:     fileMD5,
		ObjectName:  objectName,
		FileSize:    size,
		ContentType: contentType,
	}
	if err := s.repo.Asset.CreateOrRestore(asset); err != nil {
		// 并发上传同一内容时，可能另一个请求已经创建了资产。
		// 复用已存在资产，并删除当前请求刚上传但不再需要的对象。
		existing, findErr := s.repo.Asset.FindByMD5(fileMD5)
		if findErr == nil && existing != nil {
			_ = s.storage.DeleteObject(ctx, objectName)
			return existing, nil
		}
		_ = s.storage.DeleteObject(ctx, objectName)
		return nil, err
	}

	return asset, nil
}

func copyStreamToTempAndHash(r io.Reader, maxSize int64) (string, string, int64, error) {
	tmp, err := os.CreateTemp("", "vidlens_upload_*")
	if err != nil {
		return "", "", 0, err
	}
	defer tmp.Close()

	hasher := md5.New()
	reader := r
	if maxSize > 0 {
		reader = io.LimitReader(r, maxSize+1)
	}

	size, err := io.Copy(tmp, io.TeeReader(reader, hasher))
	if err != nil {
		os.Remove(tmp.Name())
		return "", "", 0, err
	}
	if maxSize > 0 && size > maxSize {
		os.Remove(tmp.Name())
		return "", "", 0, fmt.Errorf("文件大小超过限制: 最大 %d 字节", maxSize)
	}
	if size == 0 {
		os.Remove(tmp.Name())
		return "", "", 0, fmt.Errorf("文件内容为空")
	}

	return tmp.Name(), hex.EncodeToString(hasher.Sum(nil)), size, nil
}

func contentTypeForFilename(filename string) string {
	switch strings.ToLower(filepath.Ext(filename)) {
	case ".avi":
		return "video/x-msvideo"
	case ".mkv":
		return "video/x-matroska"
	case ".mov":
		return "video/quicktime"
	case ".webm":
		return "video/webm"
	default:
		return "video/mp4"
	}
}

func validateFileMD5(fileMD5 string) error {
	if len(fileMD5) != 32 {
		return fmt.Errorf("file_md5 格式错误")
	}
	if _, err := hex.DecodeString(fileMD5); err != nil {
		return fmt.Errorf("file_md5 格式错误")
	}
	return nil
}
