# Scalable Upload Video Encoder

This app uploads original videos directly to DigitalOcean Spaces with multipart upload, enqueues an encoding job in Redis/Asynq, runs FFmpeg in a Go worker, uploads the generated thumbnail and 720p HLS files back to Spaces, and plays the final HLS stream in the browser.

## Required Services

- PostgreSQL on `localhost:5432`
- Redis on `localhost:6379`
- FFmpeg and FFprobe available in `PATH`
- DigitalOcean Spaces credentials in `.env`

## Environment

```env
SPACES_ACCESS_KEY=your_spaces_access_key
SPACES_SECRET_KEY=your_spaces_secret_key
SPACES_REGION=blr1
SPACES_BUCKET=orcalexbucket
PUBLIC_SPACES_BASE_URL=https://orcalexbucket.blr1.digitaloceanspaces.com

DATABASE_URL=host=localhost port=5432 user=postgres password=postgres dbname=filesdb sslmode=disable
REDIS_ADDR=localhost:6379
REDIS_PASSWORD=
REDIS_DB=0
FFMPEG_PATH=ffmpeg
FFPROBE_PATH=ffprobe
MAX_VIDEO_SIZE_MB=2048
```

## Run Locally

Start PostgreSQL in WSL:

```bash
sudo pg_ctlcluster 16 main start
```

Start Redis:

```bash
redis-server
```

Run the app:

```bash
go run main.go
```

Open:

```text
http://localhost:8080
```

The app auto-creates the required `files`, `videos`, and `encoding_jobs` tables if they do not exist.
