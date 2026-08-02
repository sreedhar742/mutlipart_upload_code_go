package main

import (
	"context"
	"fmt"
	"github.com/joho/godotenv"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"scalable_upload/db"
	"scalable_upload/handlers"
	"scalable_upload/queue"
	"scalable_upload/storage"
	"scalable_upload/worker"
)

func main() {
	err := godotenv.Load()
	if err != nil {
		log.Println(fmt.Sprintf(".env load failed: %v", err))
	}

	storage.InitSpaces()
	db.InitDB()
	queue.Init()
	workerServer := worker.Start()

	http.HandleFunc("/upload-url", handlers.GetUploadURL)
	http.HandleFunc("/save", handlers.SaveMetadata)
	http.HandleFunc("/files", handlers.ListFiles)
	http.HandleFunc("/multipart/start", handlers.StartMultipartUpload)
	http.HandleFunc("/multipart/url", handlers.GetPartUploadURL)
	http.HandleFunc("/multipart/complete", handlers.CompleteMultipartUpload)
	http.HandleFunc("/download-url", handlers.GetDownloadURL)
	http.HandleFunc("/videos/uploads/start", handlers.StartVideoUpload)
	http.HandleFunc("/videos/uploads/part-url", handlers.GetVideoPartUploadURL)
	http.HandleFunc("/videos/uploads/complete", handlers.CompleteVideoUpload)
	http.HandleFunc("/videos", handlers.VideoRouter)
	http.HandleFunc("/videos/", handlers.VideoRouter)
	http.HandleFunc("/jobs/", handlers.JobRouter)
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, "./templates/index.html")
	})

	server := &http.Server{Addr: ":8080"}

	go func() {
		log.Println("Running on :8080 🚀")
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatal(err)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	<-stop

	log.Println("Shutting down...")
	workerServer.Shutdown()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := server.Shutdown(ctx); err != nil {
		log.Println("HTTP shutdown failed:", err)
	}
	log.Println("Stopped")
}
