package service

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"path/filepath"
	"sort"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/minio/minio-go/v7"
	"github.com/redis/go-redis/v9"
	"vid-lens/internal/model"
	"vid-lens/internal/pkg/lock"
)

const chunkUploadStateTTL = 24 * time.Hour

func (s *MediaService) MaxChunkSize() int64 {
	return s.cfg.ChunkSize
}

func uploadSpecMatches(values []interface{}, fileSize, chunkSize int64, totalChunks int) bool {
	if len(values) != 3 || values[0] == nil || values[1] == nil || values[2] == nil {
		return false
	}
	storedFileSize, err1 := strconv.ParseInt(fmt.Sprint(values[0]), 10, 64)
	storedChunkSize, err2 := strconv.ParseInt(fmt.Sprint(values[1]), 10, 64)
	storedTotalChunks, err3 := strconv.Atoi(fmt.Sprint(values[2]))
	return err1 == nil && err2 == nil && err3 == nil &&
		storedFileSize == fileSize && storedChunkSize == chunkSize && storedTotalChunks == totalChunks
}

func (s *MediaService) clearChunkUploadState(ctx context.Context, fileMD5 string) {
	if s.rdb == nil || fileMD5 == "" {
		return
	}
	key := chunkUploadKey(fileMD5)
	if err := s.rdb.Del(ctx, key, key+":status", key+":total", key+":file_size", key+":chunk_size").Err(); err != nil {
		log.Printf("[media] 清理上传状态失败（可忽略）: md5=%s err=%v", fileMD5, err)
	}
}

func chunkUploadKey(fileMD5 string) string {
	return fmt.Sprintf("upload:chunks:%s", fileMD5)
}

func (s *MediaService) resetChunkUpload(ctx context.Context, key, fileMD5 string, uploaded []string) {
	if s.storage != nil {
		for _, raw := range uploaded {
			index, err := strconv.Atoi(raw)
			if err != nil || index < 0 {
				continue
			}
			if err := s.storage.DeleteObject(ctx, fmt.Sprintf("chunks/%s/%d", fileMD5, index)); err != nil {
				log.Printf("[media] 清理不兼容上传分片失败（可忽略）: md5=%s chunk=%d err=%v", fileMD5, index, err)
			}
		}
	}
	_ = s.rdb.Del(ctx, key, key+":status", key+":total", key+":file_size", key+":chunk_size").Err()
}

func (s *MediaService) CheckUploadProgress(ctx context.Context, fileMD5 string, fileSize, chunkSize int64, totalChunks int) (map[string]any, error) {
	if s.rdb == nil {
		return nil, fmt.Errorf("Redis 上传状态存储未配置")
	}
	if err := validateFileMD5(fileMD5); err != nil {
		return nil, err
	}
	if fileSize <= 0 || chunkSize <= 0 || totalChunks <= 0 || int64(totalChunks) != (fileSize+chunkSize-1)/chunkSize {
		return nil, fmt.Errorf("上传规格无效")
	}
	if s.cfg.ChunkSize > 0 && chunkSize > s.cfg.ChunkSize {
		return nil, fmt.Errorf("分片大小超过限制: 最大 %d 字节", s.cfg.ChunkSize)
	}

	key := chunkUploadKey(fileMD5)
	uploaded, err := s.rdb.SMembers(ctx, key).Result()
	if err != nil && err != redis.Nil {
		return nil, fmt.Errorf("读取上传进度失败: %w", err)
	}
	values, err := s.rdb.MGet(ctx, key+":file_size", key+":chunk_size", key+":total").Result()
	if err != nil && err != redis.Nil {
		return nil, fmt.Errorf("读取上传规格失败: %w", err)
	}
	status, _ := s.rdb.Get(ctx, key+":status").Result()

	if (len(uploaded) > 0 || status != "") && !uploadSpecMatches(values, fileSize, chunkSize, totalChunks) {
		s.resetChunkUpload(ctx, key, fileMD5, uploaded)
		uploaded = nil
		status = ""
	}
	if status == "COMPLETED" {
		asset, err := s.repo.Asset.FindByMD5(fileMD5)
		if err != nil {
			return nil, fmt.Errorf("校验已上传文件失败: %w", err)
		}
		if asset != nil && asset.FileSize == fileSize {
			return map[string]any{"status": "completed", "uploaded": []int{}}, nil
		}
		s.clearChunkUploadState(ctx, fileMD5)
		uploaded = nil
	}

	pipe := s.rdb.TxPipeline()
	pipe.Set(ctx, key+":file_size", fileSize, chunkUploadStateTTL)
	pipe.Set(ctx, key+":chunk_size", chunkSize, chunkUploadStateTTL)
	pipe.Set(ctx, key+":total", totalChunks, chunkUploadStateTTL)
	pipe.Set(ctx, key+":status", "INIT", chunkUploadStateTTL)
	if _, err := pipe.Exec(ctx); err != nil {
		return nil, fmt.Errorf("保存上传规格失败: %w", err)
	}

	nums := make([]int, 0, len(uploaded))
	for _, value := range uploaded {
		index, err := strconv.Atoi(value)
		if err == nil && index >= 0 && index < totalChunks {
			nums = append(nums, index)
		}
	}
	sort.Ints(nums)
	return map[string]any{"status": "uploading", "uploaded": nums}, nil
}

// UploadChunk writes bytes to MinIO before marking the chunk index in Redis.
func (s *MediaService) UploadChunk(ctx context.Context, fileMD5 string, chunkNumber int, chunkData []byte, chunkSize int64) error {
	if s.rdb == nil || s.storage == nil {
		return fmt.Errorf("分片上传依赖未配置")
	}
	if err := validateFileMD5(fileMD5); err != nil {
		return err
	}
	if chunkNumber < 0 {
		return fmt.Errorf("分片序号无效")
	}
	if chunkSize <= 0 || int64(len(chunkData)) != chunkSize {
		return fmt.Errorf("分片内容为空或大小不一致")
	}
	if s.cfg.ChunkSize > 0 && chunkSize > s.cfg.ChunkSize {
		return fmt.Errorf("分片大小超过限制: 最大 %d 字节", s.cfg.ChunkSize)
	}

	objectName := fmt.Sprintf("chunks/%s/%d", fileMD5, chunkNumber)
	if err := s.storage.UploadFile(ctx, objectName, bytes.NewReader(chunkData), chunkSize, "application/octet-stream"); err != nil {
		return fmt.Errorf("分片落盘失败: %w", err)
	}
	key := chunkUploadKey(fileMD5)
	pipe := s.rdb.TxPipeline()
	pipe.SAdd(ctx, key, chunkNumber)
	pipe.Expire(ctx, key, chunkUploadStateTTL)
	if _, err := pipe.Exec(ctx); err != nil {
		_ = s.storage.DeleteObject(ctx, objectName)
		return fmt.Errorf("记录分片状态失败: %w", err)
	}
	return nil
}

func (s *MediaService) MergeChunks(ctx context.Context, userID int64, fileMD5, filename string, totalChunks int, expectedFileSize, chunkSize int64) (*UploadResult, error) {
	if s.rdb == nil || s.storage == nil {
		return nil, fmt.Errorf("分片上传依赖未配置")
	}
	if err := validateFileMD5(fileMD5); err != nil {
		return nil, err
	}
	if totalChunks <= 0 || expectedFileSize <= 0 || chunkSize <= 0 || int64(totalChunks) != (expectedFileSize+chunkSize-1)/chunkSize {
		return nil, fmt.Errorf("上传规格无效")
	}

	existingAsset, err := s.repo.Asset.FindByMD5(fileMD5)
	if err != nil {
		return nil, err
	}
	if existingAsset != nil {
		if existingAsset.FileSize != expectedFileSize {
			return nil, fmt.Errorf("同一文件指纹的已存资产大小异常: 实际 %d 字节，期望 %d 字节，请删除异常资产后重试", existingAsset.FileSize, expectedFileSize)
		}
		return s.createTaskFromAsset(userID, filename, existingAsset, model.TaskStatusPending)
	}

	mergeLock := lock.NewRedisLock(s.rdb, fmt.Sprintf("vidlens:merge:%s", fileMD5))
	acquired, err := mergeLock.TryLock(ctx, 0)
	if err != nil || !acquired {
		// 另一个请求可能已在首次查询后完成合并；复查资产可让并发请求复用结果。
		existingAsset, findErr := s.repo.Asset.FindByMD5(fileMD5)
		if findErr == nil && existingAsset != nil && existingAsset.FileSize == expectedFileSize {
			return s.createTaskFromAsset(userID, filename, existingAsset, model.TaskStatusPending)
		}
		return nil, fmt.Errorf("合并操作正在进行中，请稍后")
	}
	defer mergeLock.Unlock(ctx)

	key := chunkUploadKey(fileMD5)
	values, err := s.rdb.MGet(ctx, key+":file_size", key+":chunk_size", key+":total").Result()
	if err != nil || !uploadSpecMatches(values, expectedFileSize, chunkSize, totalChunks) {
		return nil, fmt.Errorf("上传会话规格不一致，请重新选择原文件上传")
	}
	for index := 0; index < totalChunks; index++ {
		exists, err := s.rdb.SIsMember(ctx, key, index).Result()
		if err != nil {
			return nil, fmt.Errorf("检查分片状态失败: %w", err)
		}
		if !exists {
			return nil, fmt.Errorf("分片未全部上传完成: 缺少第 %d 片", index)
		}
	}

	destination := fmt.Sprintf("videos/%s%s", uuid.New().String(), filepath.Ext(filename))
	sources := make([]minio.CopySrcOptions, 0, totalChunks)
	for index := 0; index < totalChunks; index++ {
		sources = append(sources, minio.CopySrcOptions{Bucket: s.storage.BucketName(), Object: fmt.Sprintf("chunks/%s/%d", fileMD5, index)})
	}
	size, err := s.storage.ComposeObject(ctx, destination, sources)
	if err != nil {
		return nil, fmt.Errorf("合并分片失败: %w", err)
	}
	if size != expectedFileSize {
		_ = s.storage.DeleteObject(ctx, destination)
		return nil, fmt.Errorf("合并后文件大小不一致: 实际 %d 字节，期望 %d 字节，请重新上传", size, expectedFileSize)
	}

	asset := &model.VideoAsset{FileMD5: fileMD5, ObjectName: destination, FileSize: size, ContentType: contentTypeForFilename(filename)}
	if err := s.repo.Asset.CreateOrRestore(asset); err != nil {
		_ = s.storage.DeleteObject(ctx, destination)
		return nil, err
	}
	if err := s.rdb.Set(ctx, key+":status", "COMPLETED", chunkUploadStateTTL).Err(); err != nil {
		log.Printf("[media] 保存上传完成状态失败（可忽略）: md5=%s err=%v", fileMD5, err)
	}
	s.cleanupMergedChunks(ctx, fileMD5, totalChunks)
	return s.createTaskFromAsset(userID, filename, asset, model.TaskStatusPending)
}

func (s *MediaService) cleanupMergedChunks(ctx context.Context, fileMD5 string, totalChunks int) {
	for index := 0; index < totalChunks; index++ {
		objectName := fmt.Sprintf("chunks/%s/%d", fileMD5, index)
		if err := s.storage.DeleteObject(ctx, objectName); err != nil {
			log.Printf("[media] 清理分片对象失败（可忽略）: %s err=%v", objectName, err)
		}
	}
	_ = s.rdb.Del(ctx, chunkUploadKey(fileMD5)).Err()
}
