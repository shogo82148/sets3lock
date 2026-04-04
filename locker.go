// Package sets3lock provides a distributed lock mechanism using Amazon S3.
package sets3lock

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/smithy-go"
	"github.com/google/uuid"
)

// APIClient is an interface for the S3 client used by Locker.
type APIClient interface {
	HeadObject(ctx context.Context, params *s3.HeadObjectInput, optFns ...func(*s3.Options)) (*s3.HeadObjectOutput, error)
	PutObject(ctx context.Context, params *s3.PutObjectInput, optFns ...func(*s3.Options)) (*s3.PutObjectOutput, error)
	DeleteObject(ctx context.Context, params *s3.DeleteObjectInput, optFns ...func(*s3.Options)) (*s3.DeleteObjectOutput, error)
}

type lockInfo struct {
	ID        uuid.UUID `json:"id"`
	CreatedAt time.Time `json:"created_at"`
}

// Locker provides a Lock mechanism using Amazon S3.
type Locker struct {
	client APIClient
	bucket string
	key    string
	etag   string
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

// LockWithErr try get lock.
// The return value of bool indicates whether the lock has been released. If true, it is lock granted.
func (l *Locker) LockWithErr(ctx context.Context) (bool, error) {
	id, err := uuid.NewRandom()
	if err != nil {
		return false, err
	}
	info := lockInfo{
		ID:        id,
		CreatedAt: time.Now(),
	}
	infoJSON, err := json.Marshal(info)
	if err != nil {
		return false, err
	}

	out, err := l.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:      &l.bucket,
		Key:         &l.key,
		IfNoneMatch: aws.String("*"),
		ContentType: aws.String("application/json"),
		Body:        bytes.NewReader(infoJSON),
	})
	if err == nil {
		l.etag = aws.ToString(out.ETag)
		return true, nil
	}
	if ae, ok := errors.AsType[smithy.APIError](err); ok {
		if ae.ErrorCode() != "PreconditionFailed" {
			return false, err
		}
	}

	return false, nil
}

// UnlockWithErr unlocks. It removes the lock object from S3.
func (l *Locker) UnlockWithErr(ctx context.Context) error {
	_, err := l.client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket:  &l.bucket,
		Key:     &l.key,
		IfMatch: &l.etag,
	})
	return err
}
