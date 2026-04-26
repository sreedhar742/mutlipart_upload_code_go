package main

import (
	"fmt"
	"github.com/joho/godotenv"
	"log"
	"net/http"

	"scalable_upload/db"
	"scalable_upload/handlers"
	"scalable_upload/storage"
)

func main() {
	err := godotenv.Load()
	if err != nil {
		log.Println(fmt.Sprintf(".env load failed: %v", err))
	}

	storage.InitSpaces()
	db.InitDB()

	http.HandleFunc("/upload-url", handlers.GetUploadURL)
	http.HandleFunc("/save", handlers.SaveMetadata)
	http.HandleFunc("/files", handlers.ListFiles)
	http.HandleFunc("/multipart/start", handlers.StartMultipartUpload)
	http.HandleFunc("/multipart/url", handlers.GetPartUploadURL)
	http.HandleFunc("/multipart/complete", handlers.CompleteMultipartUpload)
	http.HandleFunc("/download-url", handlers.GetDownloadURL)
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, "./templates/index.html")
	})

	log.Println("Running on :8080 🚀")
	log.Fatal(http.ListenAndServe(":8080", nil))
}
