package sets3lock

import (
	"context"
	"crypto/rand"
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/sts"
)

func TestIntegration(t *testing.T) {
	if !hasAWSAccess(t) {
		t.Skip("Skipping integration test due to lack of AWS access")
	}

	t.Run("Lock", func(t *testing.T) {
		t.Parallel()
		ctx := t.Context()
		key := rand.Text()
		u := fmt.Sprintf("s3://%s/%s", testBucket(), key)

		var wg sync.WaitGroup
		var count atomic.Int32
		for range 5 {
			wg.Go(func() {
				// Create a new locker.
				locker, err := New(ctx, u)
				if err != nil {
					t.Error(err)
				}
				_, err = locker.LockWithErr(ctx)
				if err != nil {
					t.Error(err)
				}

				t.Log("Lock acquired")
				if count.Add(1) > 1 {
					t.Error("Multiple lockers acquired the lock at the same time")
				}
				time.Sleep(100 * time.Millisecond)
				count.Add(-1)
				t.Log("Lock released")

				// Unlock the locker.
				err = locker.UnlockWithErr(ctx)
				if err != nil {
					t.Error(err)
				}
			})
		}
		wg.Wait()
	})

	t.Run("Steal", func(t *testing.T) {
		t.Parallel()
		ctx := t.Context()
		key := rand.Text()
		u := fmt.Sprintf("s3://%s/%s", testBucket(), key)

		// Create two lockers with a short grace period.
		locker1, err := New(ctx, u, WithExpireGracePeriod(2*time.Second))
		if err != nil {
			t.Fatal(err)
		}
		_, err = locker1.LockWithErr(ctx)
		if err != nil {
			t.Error(err)
		}

		locker2, err := New(ctx, u, WithExpireGracePeriod(2*time.Second))
		if err != nil {
			t.Fatal(err)
		}
		_, err = locker2.LockWithErr(ctx)
		if err != nil {
			t.Error(err)
		}

		// Attempt to unlock the first locker, which should fail because it was stolen.
		err = locker1.UnlockWithErr(ctx)
		if err == nil {
			t.Error("Expected error when unlocking a stolen lock, but got none")
		}

		// Unlock the second locker, which should succeed.
		err = locker2.UnlockWithErr(ctx)
		if err != nil {
			t.Error(err)
		}
	})
}

func testBucket() string {
	return os.Getenv("SETS3LOCK_TEST_BUCKET")
}

func hasAWSAccess(t *testing.T) bool {
	if testBucket() == "" {
		t.Log("SETS3LOCK_TEST_BUCKET environment variable is not set")
		return false
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	cfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		t.Log(err)
		return false
	}

	svc := sts.NewFromConfig(cfg)
	_, err = svc.GetCallerIdentity(ctx, &sts.GetCallerIdentityInput{})
	if err != nil {
		t.Log(err)
		return false
	}
	return true
}
