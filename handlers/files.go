package handlers

import (
	"encoding/json"
	"log"
	"net/http"

	"scalable_upload/db"
)

func SaveMetadata(w http.ResponseWriter, r *http.Request) {
	var data struct {
		Filename string `json:"filename"`
		Key      string `json:"key"`
		Size     int64  `json:"size"`
	}

	err := json.NewDecoder(r.Body).Decode(&data)
	if err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	if data.Filename == "" || data.Key == "" {
		http.Error(w, "filename and key are required", http.StatusBadRequest)
		return
	}

	log.Println("Incoming data:", data)

	_, err = db.DB.Exec(
		"INSERT INTO files (filename, object_key, size) VALUES ($1,$2,$3)",
		data.Filename, data.Key, data.Size,
	)

	if err != nil {
		log.Println("DB ERROR:", err)
		http.Error(w, err.Error(), 500)
		return
	}

	log.Println("Saved to DB ✅")

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "saved"})
}

func ListFiles(w http.ResponseWriter, r *http.Request) {
	rows, err := db.DB.Query("SELECT id, filename, object_key, size FROM files ORDER BY id DESC")
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	defer rows.Close()

	var files []map[string]interface{}

	for rows.Next() {
		var id int
		var name, key string
		var size int64

		if err := rows.Scan(&id, &name, &key, &size); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		files = append(files, map[string]interface{}{
			"id":   id,
			"name": name,
			"key":  key,
			"size": size,
		})
	}
	if err := rows.Err(); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(files)
}
