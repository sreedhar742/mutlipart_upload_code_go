package handlers

import (
	"encoding/json"
	"net/http"
	"os"
	"time"

	"scalable_upload/storage"

	"github.com/aws/aws-sdk-go-v2/service/s3"
)

func GetDownloadURL(w http.ResponseWriter, r *http.Request) {
	key := r.URL.Query().Get("key")
	bucket := os.Getenv("SPACES_BUCKET")

	if key == "" {
		http.Error(w, "key required", http.StatusBadRequest)
		return
	}
	if bucket == "" {
		http.Error(w, "SPACES_BUCKET not set", http.StatusInternalServerError)
		return
	}

	req, err := storage.PresignClient.PresignGetObject(
		r.Context(),
		&s3.GetObjectInput{
			Bucket: &bucket,
			Key:    &key,
		},
		func(opts *s3.PresignOptions) {
			opts.Expires = 10 * time.Minute
		},
	)

	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"url": req.URL,
	})
}
