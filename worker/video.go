package worker

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"scalable_upload/db"
	"scalable_upload/queue"
	"scalable_upload/storage"

	"github.com/hibiken/asynq"
)

func Start() *asynq.Server {
	server := asynq.NewServer(
		queue.RedisOpt,
		asynq.Config{Concurrency: 2},
	)

	mux := asynq.NewServeMux()
	mux.HandleFunc(queue.TypeVideoEncode, handleVideoEncode)

	go func() {
		if err := server.Run(mux); err != nil {
			log.Println("Asynq worker stopped:", err)
		}
	}()

	return server
}

func handleVideoEncode(ctx context.Context, task *asynq.Task) error {
	var payload queue.VideoEncodePayload
	if err := json.Unmarshal(task.Payload(), &payload); err != nil {
		return err
	}

	if err := updateProgress(payload.JobID, payload.VideoID, "processing", 10, ""); err != nil {
		return err
	}

	workDir, err := os.MkdirTemp("", "scalable-upload-"+payload.JobID+"-")
	if err != nil {
		failJob(payload, err)
		return err
	}
	defer os.RemoveAll(workDir)

	inputPath := filepath.Join(workDir, "original"+filepath.Ext(payload.OriginalKey))
	if err := storage.DownloadObject(ctx, payload.OriginalKey, inputPath); err != nil {
		failJob(payload, err)
		return err
	}
	updateProgress(payload.JobID, payload.VideoID, "processing", 25, "")

	duration := probeDuration(ctx, inputPath)

	thumbPath := filepath.Join(workDir, "thumbnail.jpg")
	if err := runCommand(ctx, ffmpegPath(), "-y", "-ss", "00:00:01", "-i", inputPath, "-frames:v", "1", "-q:v", "2", thumbPath); err != nil {
		failJob(payload, err)
		return err
	}
	updateProgress(payload.JobID, payload.VideoID, "processing", 40, "")

	hlsDir := filepath.Join(workDir, "hls")
	if err := os.MkdirAll(hlsDir, 0755); err != nil {
		failJob(payload, err)
		return err
	}

	renditions := []hlsRendition{
		{Name: "480p", Height: 480, Bandwidth: 1000000, AverageBandwidth: 850000, VideoBitrate: "1000k", Maxrate: "1200k", Bufsize: "2000k", Resolution: "854x480"},
		{Name: "720p", Height: 720, Bandwidth: 2500000, AverageBandwidth: 2100000, VideoBitrate: "2500k", Maxrate: "3000k", Bufsize: "5000k", Resolution: "1280x720"},
		{Name: "1080p", Height: 1080, Bandwidth: 5000000, AverageBandwidth: 4200000, VideoBitrate: "5000k", Maxrate: "6000k", Bufsize: "10000k", Resolution: "1920x1080"},
	}
	for index, rendition := range renditions {
		if err := encodeRendition(ctx, inputPath, hlsDir, rendition); err != nil {
			failJob(payload, err)
			return err
		}
		updateProgress(payload.JobID, payload.VideoID, "processing", 45+(index+1)*10, "")
	}
	if err := writeMasterPlaylist(filepath.Join(hlsDir, "master.m3u8"), renditions); err != nil {
		failJob(payload, err)
		return err
	}
	updateProgress(payload.JobID, payload.VideoID, "processing", 75, "")

	thumbnailKey := payload.OutputPrefix + "/thumbnail.jpg"
	playlistPrefix := payload.OutputPrefix + "/hls"
	playlistKey := playlistPrefix + "/master.m3u8"
	if err := storage.UploadFile(ctx, thumbnailKey, thumbPath); err != nil {
		failJob(payload, err)
		return err
	}
	if err := storage.UploadDirectory(ctx, playlistPrefix, hlsDir); err != nil {
		failJob(payload, err)
		return err
	}

	_, err = db.DB.Exec(
		`UPDATE videos SET status = 'completed', thumbnail_key = $2, playlist_key = $3, duration_seconds = $4, error_message = NULL, updated_at = NOW() WHERE id = $1`,
		payload.VideoID, thumbnailKey, playlistKey, duration,
	)
	if err != nil {
		failJob(payload, err)
		return err
	}
	return updateProgress(payload.JobID, payload.VideoID, "completed", 100, "")
}

type hlsRendition struct {
	Name             string
	Height           int
	Bandwidth        int
	AverageBandwidth int
	VideoBitrate     string
	Maxrate          string
	Bufsize          string
	Resolution       string
}

func encodeRendition(ctx context.Context, inputPath, hlsDir string, rendition hlsRendition) error {
	renditionDir := filepath.Join(hlsDir, rendition.Name)
	if err := os.MkdirAll(renditionDir, 0755); err != nil {
		return err
	}

	return runCommand(ctx, ffmpegPath(),
		"-y",
		"-i", inputPath,
		"-vf", fmt.Sprintf("scale=-2:'min(%d,ih)'", rendition.Height),
		"-c:v", "libx264",
		"-preset", "veryfast",
		"-b:v", rendition.VideoBitrate,
		"-maxrate", rendition.Maxrate,
		"-bufsize", rendition.Bufsize,
		"-c:a", "aac",
		"-b:a", "128k",
		"-hls_time", "6",
		"-hls_playlist_type", "vod",
		"-hls_segment_filename", filepath.Join(renditionDir, "segment_%03d.ts"),
		filepath.Join(renditionDir, "index.m3u8"),
	)
}

func writeMasterPlaylist(path string, renditions []hlsRendition) error {
	var builder strings.Builder
	builder.WriteString("#EXTM3U\n")
	builder.WriteString("#EXT-X-VERSION:3\n")
	for _, rendition := range renditions {
		builder.WriteString(fmt.Sprintf(
			"#EXT-X-STREAM-INF:BANDWIDTH=%d,AVERAGE-BANDWIDTH=%d,RESOLUTION=%s,CODECS=\"avc1.64001f,mp4a.40.2\"\n",
			rendition.Bandwidth,
			rendition.AverageBandwidth,
			rendition.Resolution,
		))
		builder.WriteString(rendition.Name + "/index.m3u8\n")
	}
	return os.WriteFile(path, []byte(builder.String()), 0644)
}

func updateProgress(jobID, videoID, status string, progress int, message string) error {
	_, err := db.DB.Exec(
		`UPDATE encoding_jobs SET status = $2, progress = $3, error_message = NULLIF($4, ''), updated_at = NOW() WHERE id = $1`,
		jobID, status, progress, message,
	)
	if err != nil {
		return err
	}
	_, err = db.DB.Exec(
		`UPDATE videos SET status = $2, error_message = NULLIF($3, ''), updated_at = NOW() WHERE id = $1`,
		videoID, status, message,
	)
	return err
}

func failJob(payload queue.VideoEncodePayload, err error) {
	message := err.Error()
	db.DB.Exec(`UPDATE encoding_jobs SET status = 'failed', error_message = $2, updated_at = NOW() WHERE id = $1`, payload.JobID, message)
	db.DB.Exec(`UPDATE videos SET status = 'failed', error_message = $2, updated_at = NOW() WHERE id = $1`, payload.VideoID, message)
}

func probeDuration(ctx context.Context, inputPath string) float64 {
	output, err := exec.CommandContext(ctx, ffprobePath(),
		"-v", "error",
		"-show_entries", "format=duration",
		"-of", "default=noprint_wrappers=1:nokey=1",
		inputPath,
	).Output()
	if err != nil {
		return 0
	}
	duration, _ := strconv.ParseFloat(strings.TrimSpace(string(output)), 64)
	return duration
}

func runCommand(ctx context.Context, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s failed: %w: %s", name, err, strings.TrimSpace(string(output)))
	}
	return nil
}

func ffmpegPath() string {
	if value := os.Getenv("FFMPEG_PATH"); value != "" {
		return value
	}
	return "ffmpeg"
}

func ffprobePath() string {
	if value := os.Getenv("FFPROBE_PATH"); value != "" {
		return value
	}
	return "ffprobe"
}
