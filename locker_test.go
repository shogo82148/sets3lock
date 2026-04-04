package sets3lock

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/smithy-go"
)

type mockClient struct {
	headObject   func(ctx context.Context, params *s3.HeadObjectInput, optFns ...func(*s3.Options)) (*s3.HeadObjectOutput, error)
	putObject    func(ctx context.Context, params *s3.PutObjectInput, optFns ...func(*s3.Options)) (*s3.PutObjectOutput, error)
	deleteObject func(ctx context.Context, params *s3.DeleteObjectInput, optFns ...func(*s3.Options)) (*s3.DeleteObjectOutput, error)
}

func (m *mockClient) HeadObject(ctx context.Context, params *s3.HeadObjectInput, optFns ...func(*s3.Options)) (*s3.HeadObjectOutput, error) {
	return m.headObject(ctx, params, optFns...)
}

func (m *mockClient) PutObject(ctx context.Context, params *s3.PutObjectInput, optFns ...func(*s3.Options)) (*s3.PutObjectOutput, error) {
	return m.putObject(ctx, params, optFns...)
}

func (m *mockClient) DeleteObject(ctx context.Context, params *s3.DeleteObjectInput, optFns ...func(*s3.Options)) (*s3.DeleteObjectOutput, error) {
	return m.deleteObject(ctx, params, optFns...)
}

var _ smithy.APIError = (*mockError)(nil)

type mockError struct {
	code string
}

func (e *mockError) Error() string {
	return fmt.Sprintf("sets3lock: mock error %s", e.code)
}

func (e *mockError) ErrorCode() string {
	return e.code
}

func (e *mockError) ErrorMessage() string {
	return "error message"
}

func (e *mockError) ErrorFault() smithy.ErrorFault {
	return smithy.FaultUnknown
}

func TestLocker(t *testing.T) {
	ctx := t.Context()
	client := &mockClient{
		putObject: func(ctx context.Context, params *s3.PutObjectInput, optFns ...func(*s3.Options)) (*s3.PutObjectOutput, error) {
			if aws.ToString(params.Bucket) != "bucket" {
				t.Fatalf("unexpected bucket: %s", aws.ToString(params.Bucket))
			}
			if aws.ToString(params.Key) != "key" {
				t.Fatalf("unexpected key: %s", aws.ToString(params.Key))
			}
			if aws.ToString(params.IfNoneMatch) != "*" {
				t.Fatalf("unexpected IfNoneMatch: %s", aws.ToString(params.IfNoneMatch))
			}
			return &s3.PutObjectOutput{
				ETag: aws.String("etag"),
			}, nil
		},
		deleteObject: func(ctx context.Context, params *s3.DeleteObjectInput, optFns ...func(*s3.Options)) (*s3.DeleteObjectOutput, error) {
			if aws.ToString(params.Bucket) != "bucket" {
				t.Fatalf("unexpected bucket: %s", aws.ToString(params.Bucket))
			}
			if aws.ToString(params.Key) != "key" {
				t.Fatalf("unexpected key: %s", aws.ToString(params.Key))
			}
			if aws.ToString(params.IfMatch) != "etag" {
				t.Fatalf("unexpected IfMatch: %s", aws.ToString(params.IfMatch))
			}
			return &s3.DeleteObjectOutput{}, nil
		},
	}

	locker, err := New(ctx, "s3://bucket/key", WithAPIClient(client))
	if err != nil {
		t.Fatal(err)
	}

	// Test LockWithErr
	granted, err := locker.LockWithErr(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !granted {
		t.Fatal("failed to acquire lock")
	}

	// Test UnlockWithErr
	if err := locker.UnlockWithErr(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestLocker_Blocked(t *testing.T) {
	ctx := t.Context()
	var count int
	client := &mockClient{
		putObject: func(ctx context.Context, params *s3.PutObjectInput, optFns ...func(*s3.Options)) (*s3.PutObjectOutput, error) {
			count++
			if count == 1 {
				return nil, &mockError{code: "PreconditionFailed"}
			}
			return &s3.PutObjectOutput{
				ETag: aws.String("etag"),
			}, nil
		},
		headObject: func(ctx context.Context, params *s3.HeadObjectInput, optFns ...func(*s3.Options)) (*s3.HeadObjectOutput, error) {
			return nil, errors.New("object not found")
		},
		deleteObject: func(ctx context.Context, params *s3.DeleteObjectInput, optFns ...func(*s3.Options)) (*s3.DeleteObjectOutput, error) {
			return &s3.DeleteObjectOutput{}, nil
		},
	}

	locker, err := New(ctx, "s3://bucket/key", WithAPIClient(client))
	if err != nil {
		t.Fatal(err)
	}

	// Test LockWithErr
	granted, err := locker.LockWithErr(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !granted {
		t.Fatal("failed to acquire lock")
	}

	// Test UnlockWithErr
	if err := locker.UnlockWithErr(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestLocker_NoDelay(t *testing.T) {
	ctx := t.Context()
	client := &mockClient{
		putObject: func(ctx context.Context, params *s3.PutObjectInput, optFns ...func(*s3.Options)) (*s3.PutObjectOutput, error) {
			return nil, &mockError{code: "PreconditionFailed"}
		},
	}

	locker, err := New(ctx, "s3://bucket/key", WithDelay(false), WithAPIClient(client))
	if err != nil {
		t.Fatal(err)
	}

	// Test LockWithErr
	granted, err := locker.LockWithErr(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if granted {
		t.Fatal("unexpectedly acquired lock")
	}
}

func TestLocker_Steal(t *testing.T) {
	ctx := t.Context()
	now := time.Now()

	var count int
	client := &mockClient{
		putObject: func(ctx context.Context, params *s3.PutObjectInput, optFns ...func(*s3.Options)) (*s3.PutObjectOutput, error) {
			count++
			if count == 1 {
				return nil, &mockError{code: "PreconditionFailed"}
			}
			if aws.ToString(params.IfMatch) != "etag1" {
				t.Fatalf("unexpected IfMatch: %s", aws.ToString(params.IfMatch))
			}
			return &s3.PutObjectOutput{
				ETag: aws.String("etag2"),
			}, nil
		},
		headObject: func(ctx context.Context, params *s3.HeadObjectInput, optFns ...func(*s3.Options)) (*s3.HeadObjectOutput, error) {
			return &s3.HeadObjectOutput{
				LastModified: aws.Time(now.Add(-10 * time.Second)),
				ETag:         aws.String("etag1"),
			}, nil
		},
		deleteObject: func(ctx context.Context, params *s3.DeleteObjectInput, optFns ...func(*s3.Options)) (*s3.DeleteObjectOutput, error) {
			if aws.ToString(params.IfMatch) != "etag2" {
				t.Fatalf("unexpected IfMatch: %s", aws.ToString(params.IfMatch))
			}
			return &s3.DeleteObjectOutput{}, nil
		},
	}

	locker, err := New(ctx, "s3://bucket/key", WithAPIClient(client), WithExpireGracePeriod(10*time.Second))
	if err != nil {
		t.Fatal(err)
	}

	// Test LockWithErr
	granted, err := locker.LockWithErr(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !granted {
		t.Fatal("failed to acquire lock")
	}

	// Test UnlockWithErr
	if err := locker.UnlockWithErr(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestLocker_LockerInterface(t *testing.T) {
	ctx := t.Context()
	client := &mockClient{
		putObject: func(ctx context.Context, params *s3.PutObjectInput, optFns ...func(*s3.Options)) (*s3.PutObjectOutput, error) {
			return &s3.PutObjectOutput{
				ETag: aws.String("etag"),
			}, nil
		},
		deleteObject: func(ctx context.Context, params *s3.DeleteObjectInput, optFns ...func(*s3.Options)) (*s3.DeleteObjectOutput, error) {
			return &s3.DeleteObjectOutput{}, nil
		},
	}

	locker, err := New(ctx, "s3://bucket/key", WithAPIClient(client))
	if err != nil {
		t.Fatal(err)
	}

	var l sync.Locker = locker

	l.Lock()
	defer l.Unlock()
}
