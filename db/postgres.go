package db

import (
	"database/sql"
	"log"
	"os"

	_ "github.com/lib/pq"
)

var DB *sql.DB

func InitDB() {
	connStr := os.Getenv("DATABASE_URL")
	if connStr == "" {
		connStr = "host=localhost port=5432 user=postgres password=postgres dbname=filesdb sslmode=disable"
	}

	var err error
	DB, err = sql.Open("postgres", connStr)
	if err != nil {
		log.Fatal(err)
	}

	err = DB.Ping()
	if err != nil {
		log.Fatal("DB connection failed:", err)
	}

	initSchema()

	log.Println("PostgreSQL connected 🐘")
}

func initSchema() {
	queries := []string{
		`CREATE TABLE IF NOT EXISTS files (
			id SERIAL PRIMARY KEY,
			filename TEXT NOT NULL,
			object_key TEXT NOT NULL,
			size BIGINT NOT NULL,
			created_at TIMESTAMPTZ DEFAULT NOW()
		)`,
		`CREATE TABLE IF NOT EXISTS videos (
			id TEXT PRIMARY KEY,
			original_filename TEXT NOT NULL,
			original_key TEXT NOT NULL,
			size BIGINT NOT NULL,
			status TEXT NOT NULL,
			thumbnail_key TEXT,
			playlist_key TEXT,
			duration_seconds DOUBLE PRECISION,
			error_message TEXT,
			created_at TIMESTAMPTZ DEFAULT NOW(),
			updated_at TIMESTAMPTZ DEFAULT NOW()
		)`,
		`CREATE TABLE IF NOT EXISTS encoding_jobs (
			id TEXT PRIMARY KEY,
			video_id TEXT NOT NULL REFERENCES videos(id) ON DELETE CASCADE,
			status TEXT NOT NULL,
			progress INTEGER NOT NULL DEFAULT 0,
			output_prefix TEXT NOT NULL,
			error_message TEXT,
			created_at TIMESTAMPTZ DEFAULT NOW(),
			updated_at TIMESTAMPTZ DEFAULT NOW()
		)`,
	}

	for _, query := range queries {
		if _, err := DB.Exec(query); err != nil {
			log.Fatal("DB schema init failed:", err)
		}
	}
}
