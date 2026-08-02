package queue

import (
	"encoding/json"
	"log"
	"os"
	"strconv"

	"github.com/hibiken/asynq"
)

const TypeVideoEncode = "video:encode"

type VideoEncodePayload struct {
	JobID        string `json:"job_id"`
	VideoID      string `json:"video_id"`
	OriginalKey  string `json:"original_key"`
	OutputPrefix string `json:"output_prefix"`
}

var Client *asynq.Client
var RedisOpt asynq.RedisClientOpt

func Init() {
	RedisOpt = asynq.RedisClientOpt{
		Addr:     envOrDefault("REDIS_ADDR", "localhost:6379"),
		Password: os.Getenv("REDIS_PASSWORD"),
		DB:       envIntOrDefault("REDIS_DB", 0),
	}
	Client = asynq.NewClient(RedisOpt)
}

func EnqueueVideoEncode(payload VideoEncodePayload) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	task := asynq.NewTask(TypeVideoEncode, body, asynq.MaxRetry(3))
	info, err := Client.Enqueue(task)
	if err != nil {
		return err
	}
	log.Println("Enqueued video job:", info.ID)
	return nil
}

func envOrDefault(key, fallback string) string {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	return value
}

func envIntOrDefault(key string, fallback int) int {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return fallback
	}
	return parsed
}
