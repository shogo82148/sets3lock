// Package sets3lock provides a distributed lock mechanism using Amazon S3.
package sets3lock

import (
	"context"
	"errors"
	"strings"
	"sync"

	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

// APIClient is an interface for the S3 client used by Locker.
type APIClient interface {
	HeadObject(ctx context.Context, params *s3.HeadObjectInput, optFns ...func(*s3.Options)) (*s3.HeadObjectOutput, error)
	PutObject(ctx context.Context, params *s3.PutObjectInput, optFns ...func(*s3.Options)) (*s3.PutObjectOutput, error)
}

// Locker provides a Lock mechanism using Amazon S3.
type Locker struct {
	mu     sync.Mutex
	client APIClient
	bucket string
	key    string
}

// New returns new [Locker].
func New(ctx context.Context, rawurl string, opts ...func(*Options)) (*Locker, error) {
	options := newOptions()
	for _, opt := range opts {
		opt(options)
	}

	client := options.client
	if client == nil {
		// initialize default S3 client if not provided
		cfg, err := config.LoadDefaultConfig(ctx)
		if err != nil {
			return nil, err
		}
		client = s3.NewFromConfig(cfg)
	}

	// Parse the raw URL to extract bucket and key.
	bucketAndKey, ok := strings.CutPrefix(rawurl, "s3://")
	if !ok {
		return nil, errors.New("sets3lock: invalid URL: must start with s3://")
	}
	bucket, key, ok := strings.Cut(bucketAndKey, "/")
	if !ok {
		return nil, errors.New("sets3lock: invalid URL: must contain bucket and key")
	}

	return &Locker{
		client: client,
		bucket: bucket,
		key:    key,
	}, nil
}
