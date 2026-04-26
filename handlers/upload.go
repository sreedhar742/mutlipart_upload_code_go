package handlers

import (
	"encoding/json"
	"net/http"
	"os"
	"time"

	"scalable_upload/storage"

	"github.com/aws/aws-sdk-go-v2/service/s3"
)

func GetUploadURL(w http.ResponseWriter, r *http.Request) {
	filename := r.URL.Query().Get("filename")

	if filename == "" {
		http.Error(w, "filename is required", http.StatusBadRequest)
		return
	}

	key := "uploads/" + filename
	bucket := os.Getenv("SPACES_BUCKET")

	if bucket == "" {
		http.Error(w, "SPACES_BUCKET not set", http.StatusInternalServerError)
		return
	}

	req, err := storage.PresignClient.PresignPutObject(
		r.Context(),
		&s3.PutObjectInput{
			Bucket: &bucket,
			Key:    &key,
		},
		func(opts *s3.PresignOptions) {
			opts.Expires = 5 * time.Minute
		},
	)

	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	json.NewEncoder(w).Encode(map[string]string{
		"url": req.URL,
		"key": key,
	})
}
