// Package sets3lock provides a distributed lock mechanism using Amazon S3.
package sets3lock

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/smithy-go"
	"github.com/google/uuid"
	"github.com/shogo82148/go-retry/v2"
)

var retryPolicy = &retry.Policy{
	MinDelay: 100 * time.Millisecond,
	MaxDelay: 5 * time.Second,
	Jitter:   500 * time.Microsecond,
}

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
	ctx               context.Context
	client            APIClient
	bucket            string
	key               string
	noPanic           bool
	delay             bool
	expireGracePeriod time.Duration

	mu      sync.Mutex
	etag    string
	lastErr error
}

// New returns new [Locker].
func New(ctx context.Context, rawurl string, opts ...func(*Options)) (*Locker, error) {
	options := newOptions()
	for _, opt := range opts {
		if opt != nil {
			opt(options)
		}
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
	if !ok || bucket == "" || key == "" {
		return nil, errors.New("sets3lock: invalid URL: must contain bucket and key")
	}

	return &Locker{
		ctx:               options.ctx,
		client:            client,
		bucket:            bucket,
		key:               key,
		noPanic:           options.noPanic,
		delay:             options.delay,
		expireGracePeriod: options.expireGracePeriod,
	}, nil
}

func (l *Locker) acquireLock(ctx context.Context, etag string) (bool, error) {
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

	in := &s3.PutObjectInput{
		Bucket:      &l.bucket,
		Key:         &l.key,
		ContentType: new("application/json"),
		Body:        bytes.NewReader(infoJSON),
	}
	if etag != "" {
		in.IfMatch = &etag
	} else {
		in.IfNoneMatch = new("*")
	}

	out, err := l.client.PutObject(ctx, in)
	if err == nil {
		l.mu.Lock()
		l.etag = aws.ToString(out.ETag)
		l.mu.Unlock()
		return true, nil
	}
	if ae, ok := errors.AsType[smithy.APIError](err); ok {
		if ae.ErrorCode() == "PreconditionFailed" {
			return false, nil
		}
	}
	return false, err
}

// LockWithErr try get lock.
// The return value of bool indicates whether the lock has been released. If true, it is lock granted.
func (l *Locker) LockWithErr(ctx context.Context) (bool, error) {
	granted, err := l.acquireLock(ctx, "")
	if err != nil {
		return false, err
	}
	if !l.delay {
		return granted, nil
	}
	if granted {
		return true, nil
	}

	// wait until the lock is released
	retrier := retryPolicy.Start(ctx)
	for retrier.Continue() {
		out, err := l.client.HeadObject(ctx, &s3.HeadObjectInput{
			Bucket: &l.bucket,
			Key:    &l.key,
		})
		if err != nil {
			granted, err := l.acquireLock(ctx, "")
			if err != nil {
				return false, err
			}
			if granted {
				return true, nil
			}
			continue
		}

		if l.expireGracePeriod > 0 && time.Since(aws.ToTime(out.LastModified)) > l.expireGracePeriod {
			granted, err := l.acquireLock(ctx, aws.ToString(out.ETag))
			if err != nil {
				return false, err
			}
			if granted {
				return true, nil
			}
		}
	}

	if err := ctx.Err(); err != nil {
		return false, err
	}
	return false, errors.New("sets3lock: failed to acquire lock")
}

// Lock for implements [sync.Locker].
func (l *Locker) Lock() {
	granted, err := l.LockWithErr(l.ctx)
	if err != nil {
		l.bailout(err)
	}
	if !granted {
		l.bailout(errors.New("sets3lock: failed to acquire lock"))
	}
}

// UnlockWithErr unlocks. It removes the lock object from S3.
func (l *Locker) UnlockWithErr(ctx context.Context) error {
	l.mu.Lock()
	etag := l.etag
	l.mu.Unlock()
	_, err := l.client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket:  &l.bucket,
		Key:     &l.key,
		IfMatch: &etag,
	})
	return err
}

// Unlock implements [sync.Locker].
func (l *Locker) Unlock() {
	err := l.UnlockWithErr(l.ctx)
	if err != nil {
		l.bailout(err)
	}
}

func (l *Locker) LastErr() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.lastErr
}

func (l *Locker) ClearLastErr() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.lastErr = nil
}

type bailoutErr struct {
	err error
}

func (l *Locker) bailout(err error) {
	l.mu.Lock()
	l.lastErr = err
	l.mu.Unlock()

	if !l.noPanic {
		panic(bailoutErr{err})
	}
}

// Recover for [Locker.Lock]() and [Locker.Unlock()]() panic.
func Recover(e any) error {
	if e != nil {
		b, ok := e.(bailoutErr)
		if !ok {
			panic(e)
		}
		return b.err
	}
	return nil
}
