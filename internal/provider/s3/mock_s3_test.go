package s3

import (
	"encoding/xml"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	s3sdk "github.com/aws/aws-sdk-go-v2/service/s3"
)

// mockS3 is an in-memory S3 server for unit tests.
type mockS3 struct {
	mu   sync.Mutex
	data map[string][]byte
}

func newMockS3Server(t *testing.T) (*httptest.Server, *Backend) {
	t.Helper()
	mock := &mockS3{data: make(map[string][]byte)}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Parse bucket and key from path: /bucket/key
		parts := strings.SplitN(strings.TrimPrefix(r.URL.Path, "/"), "/", 2)
		bucket := ""
		key := ""
		if len(parts) >= 1 {
			bucket = parts[0]
		}
		if len(parts) >= 2 {
			key = parts[1]
		}
		_ = bucket

		mock.mu.Lock()
		defer mock.mu.Unlock()

		switch r.Method {
		case "PUT":
			body, _ := io.ReadAll(r.Body)
			mock.data[key] = body
			w.WriteHeader(200)

		case "GET":
			// Check for list-type=2 query param (ListObjectsV2)
			if r.URL.Query().Get("list-type") == "2" {
				prefix := r.URL.Query().Get("prefix")
				type ListResult struct {
					XMLName  xml.Name `xml:"ListBucketResult"`
					Contents []struct {
						Key  string `xml:"Key"`
						Size int64  `xml:"Size"`
					} `xml:"Contents"`
				}
				var result ListResult
				for k, v := range mock.data {
					if strings.HasPrefix(k, prefix) {
						result.Contents = append(result.Contents, struct {
							Key  string `xml:"Key"`
							Size int64  `xml:"Size"`
						}{Key: k, Size: int64(len(v))})
					}
				}
				w.Header().Set("Content-Type", "application/xml")
				xml.NewEncoder(w).Encode(result)
				return
			}

			data, ok := mock.data[key]
			if !ok {
				w.WriteHeader(404)
				w.Write([]byte(`<?xml version="1.0"?><Error><Code>NoSuchKey</Code></Error>`))
				return
			}
			w.Write(data)

		case "DELETE":
			delete(mock.data, key)
			w.WriteHeader(204)

		case "HEAD":
			if _, ok := mock.data[key]; !ok {
				w.WriteHeader(404)
				return
			}
			w.WriteHeader(200)

		default:
			w.WriteHeader(405)
		}
	}))

	// Create S3 client pointing at mock server
	cfg, _ := awsconfig.LoadDefaultConfig(t.Context(),
		awsconfig.WithRegion("us-east-1"),
		awsconfig.WithCredentialsProvider(credentials.NewStaticCredentialsProvider("test", "test", "")),
	)

	client := s3sdk.NewFromConfig(cfg, func(o *s3sdk.Options) {
		o.BaseEndpoint = aws.String(srv.URL)
		o.UsePathStyle = true
	})

	b := newFromClient(client, "test-bucket")
	return srv, b
}
