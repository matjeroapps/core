package commerce

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

type S3Config struct {
	Endpoint        string
	Region          string
	Bucket          string
	AccessKeyID     string
	SecretAccessKey string
	ForcePathStyle  bool
	PublicBaseURL   string
	MaxBytes        int64
	URLTTL          time.Duration
}

type S3Storage struct {
	cfg           S3Config
	client        *s3.Client
	presignClient *s3.PresignClient
}

func NewS3Storage(cfg S3Config) *S3Storage {
	if cfg.Region == "" {
		cfg.Region = "us-east-1"
	}
	if cfg.URLTTL <= 0 {
		cfg.URLTTL = 15 * time.Minute
	}
	if cfg.MaxBytes <= 0 {
		cfg.MaxBytes = 10 * 1024 * 1024 // 10MB
	}

	customResolver := aws.EndpointResolverWithOptionsFunc(func(service, region string, options ...interface{}) (aws.Endpoint, error) {
		if cfg.Endpoint != "" {
			return aws.Endpoint{
				PartitionID:       "aws",
				URL:               cfg.Endpoint,
				SigningRegion:     cfg.Region,
				HostnameImmutable: cfg.ForcePathStyle,
			}, nil
		}
		return aws.Endpoint{}, &aws.EndpointNotFoundError{}
	})

	awsCfg := aws.Config{
		Region:                      cfg.Region,
		Credentials:                 credentials.NewStaticCredentialsProvider(cfg.AccessKeyID, cfg.SecretAccessKey, ""),
		EndpointResolverWithOptions: customResolver,
	}

	client := s3.NewFromConfig(awsCfg, func(o *s3.Options) {
		o.UsePathStyle = cfg.ForcePathStyle
	})

	presignClient := s3.NewPresignClient(client)

	return &S3Storage{
		cfg:           cfg,
		client:        client,
		presignClient: presignClient,
	}
}

func (s *S3Storage) Config() S3Config {
	return s.cfg
}

func (s *S3Storage) PresignPutObject(ctx context.Context, storageKey, contentType string) (string, error) {
	req, err := s.presignClient.PresignPutObject(ctx, &s3.PutObjectInput{
		Bucket:      aws.String(s.cfg.Bucket),
		Key:         aws.String(storageKey),
		ContentType: aws.String(contentType),
	}, s3.WithPresignExpires(s.cfg.URLTTL))
	if err != nil {
		return "", fmt.Errorf("presign put object: %w", err)
	}
	return req.URL, nil
}

func (s *S3Storage) HeadObject(ctx context.Context, storageKey string) (*s3.HeadObjectOutput, error) {
	out, err := s.client.HeadObject(ctx, &s3.HeadObjectInput{
		Bucket: aws.String(s.cfg.Bucket),
		Key:    aws.String(storageKey),
	})
	if err != nil {
		return nil, fmt.Errorf("head object %s: %w", storageKey, err)
	}
	return out, nil
}

func (s *S3Storage) DeleteObject(ctx context.Context, storageKey string) error {
	_, err := s.client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(s.cfg.Bucket),
		Key:    aws.String(storageKey),
	})
	if err != nil {
		return fmt.Errorf("delete object %s: %w", storageKey, err)
	}
	return nil
}

func (s *S3Storage) ResolvePublicURI(storageKey string) string {
	if s.cfg.PublicBaseURL != "" {
		base := strings.TrimRight(s.cfg.PublicBaseURL, "/")
		key := strings.TrimLeft(storageKey, "/")
		return fmt.Sprintf("%s/%s", base, key)
	}
	if s.cfg.Endpoint != "" {
		base := strings.TrimRight(s.cfg.Endpoint, "/")
		return fmt.Sprintf("%s/%s/%s", base, s.cfg.Bucket, strings.TrimLeft(storageKey, "/"))
	}
	return fmt.Sprintf("https://%s.s3.%s.amazonaws.com/%s", s.cfg.Bucket, s.cfg.Region, strings.TrimLeft(storageKey, "/"))
}
