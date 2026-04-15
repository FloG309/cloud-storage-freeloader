package s3

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"

	"github.com/FloG309/cloud-storage-freeloader/internal/provider"
)

// Backend implements provider.StorageBackend for S3-compatible storage.
type Backend struct {
	store    provider.StorageBackend // for unit tests (NewWithStore)
	client   *s3.Client             // for real S3 (NewFromConfig)
	bucket   string
	capacity int64
}

// NewWithStore creates an S3 backend backed by the given store (for testing).
func NewWithStore(store provider.StorageBackend) *Backend {
	return &Backend{store: store}
}

// NewFromConfig creates a real S3 backend from config.
func NewFromConfig(cfg map[string]string) (*Backend, error) {
	bucket := cfg["bucket"]
	if bucket == "" {
		return nil, fmt.Errorf("s3: bucket is required")
	}
	endpoint := cfg["endpoint"]
	region := cfg["region"]
	if region == "" {
		region = "us-east-1"
	}
	keyID := cfg["key_id"]
	appKey := cfg["application_key"]

	awsCfg, err := awsconfig.LoadDefaultConfig(context.Background(),
		awsconfig.WithRegion(region),
		awsconfig.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(keyID, appKey, "")),
	)
	if err != nil {
		return nil, fmt.Errorf("s3: load aws config: %w", err)
	}

	client := s3.NewFromConfig(awsCfg, func(o *s3.Options) {
		if endpoint != "" {
			o.BaseEndpoint = aws.String("https://" + endpoint)
			if strings.HasPrefix(endpoint, "http") {
				o.BaseEndpoint = aws.String(endpoint)
			}
		}
		o.UsePathStyle = true
	})

	capacity := int64(10 * 1024 * 1024 * 1024) // 10GB default (B2 free tier)

	return &Backend{client: client, bucket: bucket, capacity: capacity}, nil
}

// newFromClient creates a backend with an existing S3 client (for mock tests).
func newFromClient(client *s3.Client, bucket string) *Backend {
	return &Backend{client: client, bucket: bucket, capacity: 10 * 1024 * 1024 * 1024}
}

func (b *Backend) Put(ctx context.Context, key string, data []byte) error {
	if b.store != nil {
		return b.store.Put(ctx, key, data)
	}
	_, err := b.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket: aws.String(b.bucket),
		Key:    aws.String(key),
		Body:   bytes.NewReader(data),
	})
	return err
}

func (b *Backend) Get(ctx context.Context, key string) ([]byte, error) {
	if b.store != nil {
		return b.store.Get(ctx, key)
	}
	out, err := b.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(b.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return nil, fmt.Errorf("s3 get %s: %w", key, err)
	}
	defer out.Body.Close()
	return io.ReadAll(out.Body)
}

func (b *Backend) Delete(ctx context.Context, key string) error {
	if b.store != nil {
		return b.store.Delete(ctx, key)
	}
	_, err := b.client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(b.bucket),
		Key:    aws.String(key),
	})
	return err
}

func (b *Backend) Exists(ctx context.Context, key string) (bool, error) {
	if b.store != nil {
		return b.store.Exists(ctx, key)
	}
	_, err := b.client.HeadObject(ctx, &s3.HeadObjectInput{
		Bucket: aws.String(b.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		var nsk *types.NotFound
		if ok := errors_As(err, &nsk); ok {
			return false, nil
		}
		// Also check for generic 404
		if strings.Contains(err.Error(), "NotFound") || strings.Contains(err.Error(), "404") || strings.Contains(err.Error(), "NoSuchKey") {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

func (b *Backend) List(ctx context.Context, prefix string) ([]string, error) {
	if b.store != nil {
		return b.store.List(ctx, prefix)
	}
	var keys []string
	paginator := s3.NewListObjectsV2Paginator(b.client, &s3.ListObjectsV2Input{
		Bucket: aws.String(b.bucket),
		Prefix: aws.String(prefix),
	})
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("s3 list: %w", err)
		}
		for _, obj := range page.Contents {
			keys = append(keys, *obj.Key)
		}
	}
	return keys, nil
}

func (b *Backend) Available(ctx context.Context) (int64, error) {
	if b.store != nil {
		return b.store.Available(ctx)
	}
	return b.capacity, nil
}

func (b *Backend) Close() error {
	if b.store != nil {
		return b.store.Close()
	}
	return nil
}

// Profile returns the provider's constraint profile.
func (b *Backend) Profile() provider.ProviderProfile {
	return provider.ProviderProfile{
		DailyEgressLimit: 0,
		MaxFileSize:      0,
	}
}

// errors_As is a helper to avoid importing errors in this file
// (workaround for the errors.As type assertion)
func errors_As(err error, target interface{}) bool {
	// We use string matching instead since the S3 SDK wraps errors
	return false
}

var _ provider.StorageBackend = (*Backend)(nil)
