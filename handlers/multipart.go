package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"sort"

	"scalable_upload/storage"

	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
)

func int32Ptr(s string) *int32 {
	var i int32
	fmt.Sscanf(s, "%d", &i)
	return &i
}

// STEP 1: Start multipart upload
func StartMultipartUpload(w http.ResponseWriter, r *http.Request) {
	filename := r.URL.Query().Get("filename")
	bucket := os.Getenv("SPACES_BUCKET")
	if filename == "" {
		http.Error(w, "filename is required", http.StatusBadRequest)
		return
	}
	if bucket == "" {
		http.Error(w, "SPACES_BUCKET not set", http.StatusInternalServerError)
		return
	}

	key := "uploads/" + filename

	resp, err := storage.Client.CreateMultipartUpload(context.TODO(), &s3.CreateMultipartUploadInput{
		Bucket: &bucket,
		Key:    &key,
	})

	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}

	json.NewEncoder(w).Encode(map[string]string{
		"uploadId": *resp.UploadId,
		"key":      key,
	})
}

// STEP 2: Get presigned URL for each part
func GetPartUploadURL(w http.ResponseWriter, r *http.Request) {
	bucket := os.Getenv("SPACES_BUCKET")
	key := r.URL.Query().Get("key")
	uploadId := r.URL.Query().Get("uploadId")
	partNumber := r.URL.Query().Get("partNumber")
	if bucket == "" {
		http.Error(w, "SPACES_BUCKET not set", http.StatusInternalServerError)
		return
	}
	if key == "" || uploadId == "" || partNumber == "" {
		http.Error(w, "key, uploadId and partNumber are required", http.StatusBadRequest)
		return
	}

	req, err := storage.PresignClient.PresignUploadPart(
		r.Context(),
		&s3.UploadPartInput{
			Bucket:     &bucket,
			Key:        &key,
			UploadId:   &uploadId,
			PartNumber: int32Ptr(partNumber),
		},
	)

	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}

	json.NewEncoder(w).Encode(map[string]string{
		"url": req.URL,
	})
}

// STEP 3: Complete upload
func CompleteMultipartUpload(w http.ResponseWriter, r *http.Request) {
	bucket := os.Getenv("SPACES_BUCKET")
	if bucket == "" {
		http.Error(w, "SPACES_BUCKET not set", http.StatusInternalServerError)
		return
	}

	var data struct {
		Key      string `json:"key"`
		UploadId string `json:"uploadId"`
		Parts    []struct {
			ETag       string `json:"ETag"`
			PartNumber int32  `json:"PartNumber"`
		} `json:"parts"`
	}

	if err := json.NewDecoder(r.Body).Decode(&data); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if data.Key == "" || data.UploadId == "" || len(data.Parts) == 0 {
		http.Error(w, "key, uploadId and parts are required", http.StatusBadRequest)
		return
	}

	sort.Slice(data.Parts, func(i, j int) bool {
		return data.Parts[i].PartNumber < data.Parts[j].PartNumber
	})

	var completedParts []types.CompletedPart

	for _, p := range data.Parts {
		etag := p.ETag
		completedParts = append(completedParts, types.CompletedPart{
			ETag:       &etag,
			PartNumber: &p.PartNumber,
		})
	}

	_, err := storage.Client.CompleteMultipartUpload(context.TODO(), &s3.CompleteMultipartUploadInput{
		Bucket:   &bucket,
		Key:      &data.Key,
		UploadId: &data.UploadId,
		MultipartUpload: &types.CompletedMultipartUpload{
			Parts: completedParts,
		},
	})

	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"status": "completed",
		"key":    data.Key,
	})
}
