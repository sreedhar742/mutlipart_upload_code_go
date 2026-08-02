package handlers

import (
	"context"
	"database/sql"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"scalable_upload/db"
	"scalable_upload/queue"
	"scalable_upload/storage"

	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/google/uuid"
)

var allowedVideoExts = map[string]bool{
	".mp4":  true,
	".mov":  true,
	".m4v":  true,
	".webm": true,
	".mkv":  true,
}

func StartVideoUpload(w http.ResponseWriter, r *http.Request) {
	filename := r.URL.Query().Get("filename")
	size, _ := strconv.ParseInt(r.URL.Query().Get("size"), 10, 64)
	if filename == "" || size <= 0 {
		http.Error(w, "filename and positive size are required", http.StatusBadRequest)
		return
	}
	if !allowedVideoExts[strings.ToLower(filepath.Ext(filename))] {
		http.Error(w, "unsupported video type", http.StatusBadRequest)
		return
	}
	if size > maxVideoSizeBytes() {
		http.Error(w, "video exceeds maximum allowed size", http.StatusBadRequest)
		return
	}

	videoID := uuid.NewString()
	key := "originals/" + videoID + "/" + safeFilename(filename)
	resp, err := storage.Client.CreateMultipartUpload(context.TODO(), &s3.CreateMultipartUploadInput{
		Bucket: &storage.Bucket,
		Key:    &key,
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	writeJSON(w, map[string]string{
		"videoId":  videoID,
		"uploadId": *resp.UploadId,
		"key":      key,
	})
}

func GetVideoPartUploadURL(w http.ResponseWriter, r *http.Request) {
	key := r.URL.Query().Get("key")
	uploadID := r.URL.Query().Get("uploadId")
	partNumberValue, err := strconv.ParseInt(r.URL.Query().Get("partNumber"), 10, 32)
	if key == "" || uploadID == "" || err != nil || partNumberValue < 1 || partNumberValue > 10000 {
		http.Error(w, "key, uploadId and valid partNumber are required", http.StatusBadRequest)
		return
	}
	partNumber := int32(partNumberValue)

	req, err := storage.PresignClient.PresignUploadPart(
		r.Context(),
		&s3.UploadPartInput{
			Bucket:     &storage.Bucket,
			Key:        &key,
			UploadId:   &uploadID,
			PartNumber: &partNumber,
		},
	)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	writeJSON(w, map[string]string{"url": req.URL})
}

func CompleteVideoUpload(w http.ResponseWriter, r *http.Request) {
	var data struct {
		VideoID  string `json:"videoId"`
		Filename string `json:"filename"`
		Key      string `json:"key"`
		UploadID string `json:"uploadId"`
		Size     int64  `json:"size"`
		Parts    []struct {
			ETag       string `json:"ETag"`
			PartNumber int32  `json:"PartNumber"`
		} `json:"parts"`
	}
	if err := json.NewDecoder(r.Body).Decode(&data); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if data.VideoID == "" || data.Filename == "" || data.Key == "" || data.UploadID == "" || data.Size <= 0 || len(data.Parts) == 0 {
		http.Error(w, "videoId, filename, key, uploadId, size and parts are required", http.StatusBadRequest)
		return
	}

	completedParts, err := completedUploadParts(data.Parts)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	_, err = storage.Client.CompleteMultipartUpload(context.TODO(), &s3.CompleteMultipartUploadInput{
		Bucket:   &storage.Bucket,
		Key:      &data.Key,
		UploadId: &data.UploadID,
		MultipartUpload: &types.CompletedMultipartUpload{
			Parts: completedParts,
		},
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	jobID := uuid.NewString()
	outputPrefix := "videos/" + data.VideoID
	tx, err := db.DB.Begin()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer tx.Rollback()

	_, err = tx.Exec(
		`INSERT INTO videos (id, original_filename, original_key, size, status)
		 VALUES ($1, $2, $3, $4, 'queued')`,
		data.VideoID, data.Filename, data.Key, data.Size,
	)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	_, err = tx.Exec(
		`INSERT INTO encoding_jobs (id, video_id, status, progress, output_prefix)
		 VALUES ($1, $2, 'queued', 0, $3)`,
		jobID, data.VideoID, outputPrefix,
	)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if err := tx.Commit(); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	err = queue.EnqueueVideoEncode(queue.VideoEncodePayload{
		JobID:        jobID,
		VideoID:      data.VideoID,
		OriginalKey:  data.Key,
		OutputPrefix: outputPrefix,
	})
	if err != nil {
		markJobFailed(jobID, data.VideoID, err.Error())
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	writeJSON(w, map[string]string{
		"videoId": data.VideoID,
		"jobId":   jobID,
		"status":  "queued",
	})
}

func ListVideos(w http.ResponseWriter, r *http.Request) {
	rows, err := db.DB.Query(`SELECT id, original_filename, size, status, COALESCE(thumbnail_key, ''), COALESCE(playlist_key, ''), COALESCE(error_message, '') FROM videos ORDER BY created_at DESC`)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var videos []map[string]interface{}
	for rows.Next() {
		var id, filename, status, thumbnailKey, playlistKey, errorMessage string
		var size int64
		if err := rows.Scan(&id, &filename, &size, &status, &thumbnailKey, &playlistKey, &errorMessage); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		videos = append(videos, videoResponse(id, filename, size, status, thumbnailKey, playlistKey, errorMessage))
	}
	writeJSON(w, videos)
}

func VideoRouter(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/videos")
	path = strings.Trim(path, "/")
	if path == "" {
		if r.Method == http.MethodGet {
			ListVideos(w, r)
			return
		}
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	parts := strings.Split(path, "/")
	videoID := parts[0]
	if len(parts) == 2 && parts[1] == "playlist" && r.Method == http.MethodGet {
		GetVideoPlaylist(w, r, videoID)
		return
	}
	if len(parts) == 1 {
		switch r.Method {
		case http.MethodGet:
			GetVideo(w, r, videoID)
		case http.MethodDelete:
			DeleteVideo(w, r, videoID)
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
		return
	}
	http.NotFound(w, r)
}

func GetVideo(w http.ResponseWriter, r *http.Request, videoID string) {
	video, err := findVideo(videoID)
	if err == sql.ErrNoRows {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, video)
}

func GetVideoPlaylist(w http.ResponseWriter, r *http.Request, videoID string) {
	var playlistKey string
	err := db.DB.QueryRow(`SELECT COALESCE(playlist_key, '') FROM videos WHERE id = $1`, videoID).Scan(&playlistKey)
	if err == sql.ErrNoRows || playlistKey == "" {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, storage.PublicURL(playlistKey), http.StatusFound)
}

func DeleteVideo(w http.ResponseWriter, r *http.Request, videoID string) {
	var originalKey, outputPrefix string
	err := db.DB.QueryRow(
		`SELECT v.original_key, COALESCE(j.output_prefix, '')
		 FROM videos v
		 LEFT JOIN encoding_jobs j ON j.video_id = v.id
		 WHERE v.id = $1
		 ORDER BY j.created_at DESC
		 LIMIT 1`,
		videoID,
	).Scan(&originalKey, &outputPrefix)
	if err == sql.ErrNoRows {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if err := storage.DeleteObject(r.Context(), originalKey); err != nil {
		log.Println("Delete original object failed:", err)
	}
	if err := storage.DeletePrefix(r.Context(), outputPrefix); err != nil {
		log.Println("Delete output prefix failed:", err)
	}

	_, err = db.DB.Exec(`DELETE FROM videos WHERE id = $1`, videoID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]string{"status": "deleted"})
}

func JobRouter(w http.ResponseWriter, r *http.Request) {
	jobID := strings.Trim(strings.TrimPrefix(r.URL.Path, "/jobs/"), "/")
	if jobID == "" || r.Method != http.MethodGet {
		http.Error(w, "job id required", http.StatusBadRequest)
		return
	}

	var videoID, status, outputPrefix, errorMessage string
	var progress int
	err := db.DB.QueryRow(`SELECT video_id, status, progress, output_prefix, COALESCE(error_message, '') FROM encoding_jobs WHERE id = $1`, jobID).
		Scan(&videoID, &status, &progress, &outputPrefix, &errorMessage)
	if err == sql.ErrNoRows {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	writeJSON(w, map[string]interface{}{
		"id":           jobID,
		"videoId":      videoID,
		"status":       status,
		"progress":     progress,
		"outputPrefix": outputPrefix,
		"error":        errorMessage,
	})
}

func completedUploadParts(parts []struct {
	ETag       string `json:"ETag"`
	PartNumber int32  `json:"PartNumber"`
}) ([]types.CompletedPart, error) {
	sort.Slice(parts, func(i, j int) bool {
		return parts[i].PartNumber < parts[j].PartNumber
	})

	seen := make(map[int32]struct{}, len(parts))
	completed := make([]types.CompletedPart, 0, len(parts))
	for _, part := range parts {
		if part.PartNumber < 1 || part.PartNumber > 10000 {
			return nil, errBadRequest("part numbers must be between 1 and 10000")
		}
		if _, ok := seen[part.PartNumber]; ok {
			return nil, errBadRequest("duplicate part numbers are not allowed")
		}
		seen[part.PartNumber] = struct{}{}

		etag := strings.Trim(part.ETag, `"`)
		if etag == "" {
			return nil, errBadRequest("each part must include an ETag")
		}
		completed = append(completed, types.CompletedPart{ETag: &etag, PartNumber: &part.PartNumber})
	}
	return completed, nil
}

type errBadRequest string

func (e errBadRequest) Error() string { return string(e) }

func findVideo(videoID string) (map[string]interface{}, error) {
	var id, filename, status, thumbnailKey, playlistKey, errorMessage string
	var size int64
	err := db.DB.QueryRow(`SELECT id, original_filename, size, status, COALESCE(thumbnail_key, ''), COALESCE(playlist_key, ''), COALESCE(error_message, '') FROM videos WHERE id = $1`, videoID).
		Scan(&id, &filename, &size, &status, &thumbnailKey, &playlistKey, &errorMessage)
	if err != nil {
		return nil, err
	}
	return videoResponse(id, filename, size, status, thumbnailKey, playlistKey, errorMessage), nil
}

func videoResponse(id, filename string, size int64, status, thumbnailKey, playlistKey, errorMessage string) map[string]interface{} {
	resp := map[string]interface{}{
		"id":       id,
		"filename": filename,
		"size":     size,
		"status":   status,
		"error":    errorMessage,
	}
	if thumbnailKey != "" {
		resp["thumbnailUrl"] = storage.PublicURL(thumbnailKey)
	}
	if playlistKey != "" {
		resp["playlistUrl"] = storage.PublicURL(playlistKey)
	}
	return resp
}

func maxVideoSizeBytes() int64 {
	mb, err := strconv.ParseInt(os.Getenv("MAX_VIDEO_SIZE_MB"), 10, 64)
	if err != nil || mb <= 0 {
		mb = 2048
	}
	return mb * 1024 * 1024
}

func safeFilename(filename string) string {
	name := filepath.Base(filename)
	name = strings.ReplaceAll(name, " ", "_")
	return name
}

func markJobFailed(jobID, videoID, message string) {
	db.DB.Exec(`UPDATE encoding_jobs SET status = 'failed', progress = 0, error_message = $2, updated_at = NOW() WHERE id = $1`, jobID, message)
	db.DB.Exec(`UPDATE videos SET status = 'failed', error_message = $2, updated_at = NOW() WHERE id = $1`, videoID, message)
}

func writeJSON(w http.ResponseWriter, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(data)
}
