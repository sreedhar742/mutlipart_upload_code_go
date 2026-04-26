package storage

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

var Client *s3.Client
var PresignClient *s3.PresignClient

func InitSpaces() {
	accessKey := os.Getenv("SPACES_ACCESS_KEY")
	secretKey := os.Getenv("SPACES_SECRET_KEY")
	region := os.Getenv("SPACES_REGION")
	bucket := os.Getenv("SPACES_BUCKET")

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
}
