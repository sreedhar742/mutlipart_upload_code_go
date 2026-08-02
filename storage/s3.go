package storage

import (
	"context"
	"fmt"
	"io"
	"log"
	"mime"
	"os"
	"path/filepath"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
)

var Client *s3.Client
var PresignClient *s3.PresignClient
var Bucket string
var PublicBaseURL string

func InitSpaces() {
	accessKey := os.Getenv("SPACES_ACCESS_KEY")
	secretKey := os.Getenv("SPACES_SECRET_KEY")
	region := os.Getenv("SPACES_REGION")
	bucket := os.Getenv("SPACES_BUCKET")
	publicBaseURL := os.Getenv("PUBLIC_SPACES_BASE_URL")

	var missing []string
	if accessKey == "" {
		missing = append(missing, "SPACES_ACCESS_KEY")
	}
	if secretKey == "" {
		missing = append(missing, "SPACES_SECRET_KEY")
	}
	if region == "" {
		missing = append(missing, "SPACES_REGION")
	}
	if bucket == "" {
		missing = append(missing, "SPACES_BUCKET")
	}
	if len(missing) > 0 {
		log.Fatal(fmt.Sprintf("missing required env vars: %v", missing))
	}

	endpoint := "https://" + region + ".digitaloceanspaces.com"
	if publicBaseURL == "" {
		publicBaseURL = "https://" + bucket + "." + region + ".digitaloceanspaces.com"
	}

	cfg, err := config.LoadDefaultConfig(context.TODO(),
		config.WithRegion("us-east-1"),
		config.WithCredentialsProvider(
			credentials.NewStaticCredentialsProvider(accessKey, secretKey, ""),
		),
	)

	if err != nil {
		log.Fatal(err)
	}

	Client = s3.NewFromConfig(cfg, func(o *s3.Options) {
		o.BaseEndpoint = aws.String(endpoint)
		o.UsePathStyle = false
	})

	PresignClient = s3.NewPresignClient(Client)
	Bucket = bucket
	PublicBaseURL = strings.TrimRight(publicBaseURL, "/")
}

func PublicURL(key string) string {
	if key == "" {
		return ""
	}
	return PublicBaseURL + "/" + strings.TrimLeft(key, "/")
}

func DownloadObject(ctx context.Context, key, destPath string) error {
	resp, err := Client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: &Bucket,
		Key:    &key,
	})
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	out, err := os.Create(destPath)
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, resp.Body)
	return err
}

func UploadFile(ctx context.Context, key, path string) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()

	contentType := contentTypeForPath(path)
	_, err = Client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:      &Bucket,
		Key:         &key,
		Body:        file,
		ContentType: &contentType,
		ACL:         "public-read",
	})
	return err
}

func UploadDirectory(ctx context.Context, prefix, dir string) error {
	return filepath.WalkDir(dir, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}

		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		key := strings.TrimRight(prefix, "/") + "/" + filepath.ToSlash(rel)
		return UploadFile(ctx, key, path)
	})
}

func DeleteObject(ctx context.Context, key string) error {
	if key == "" {
		return nil
	}
	_, err := Client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: &Bucket,
		Key:    &key,
	})
	return err
}

func DeletePrefix(ctx context.Context, prefix string) error {
	if prefix == "" {
		return nil
	}

	paginator := s3.NewListObjectsV2Paginator(Client, &s3.ListObjectsV2Input{
		Bucket: &Bucket,
		Prefix: &prefix,
	})

	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return err
		}
		for _, object := range page.Contents {
			if object.Key == nil {
				continue
			}
			_, err := Client.DeleteObjects(ctx, &s3.DeleteObjectsInput{
				Bucket: &Bucket,
				Delete: &types.Delete{
					Objects: []types.ObjectIdentifier{{Key: object.Key}},
					Quiet:   aws.Bool(true),
				},
			})
			if err != nil {
				return err
			}
		}
	}
	return nil
}

func contentTypeForPath(path string) string {
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".m3u8":
		return "application/vnd.apple.mpegurl"
	case ".ts":
		return "video/mp2t"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	default:
		if contentType := mime.TypeByExtension(ext); contentType != "" {
			return contentType
		}
		return "application/octet-stream"
	}
}
