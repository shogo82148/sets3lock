package sets3lock

import (
	"context"
	"time"
)

// Options are for changing the behavior of the lock mechanism.
type Options struct {
	ctx               context.Context
	client            APIClient
	noPanic           bool
	delay             bool
	expireGracePeriod time.Duration
}

func newOptions() *Options {
	return &Options{
		ctx:   context.Background(),
		delay: true,
	}
}

// WithContext allows you to specify a context for the locker. The context will be used for all operations of the locker.
func WithContext(ctx context.Context) func(*Options) {
	if ctx == nil {
		panic("sets3lock: context cannot be nil")
	}
	return func(o *Options) {
		o.ctx = ctx
	}
}

// WithAPIClient allows you to specify a custom S3 client for the locker.
func WithAPIClient(client APIClient) func(*Options) {
	return func(o *Options) {
		o.client = client
	}
}

// WithNoPanic changes the behavior so that it does not panic if an error occurs in the [Locker.Lock]() and [Locker.Unlock]() functions.
// Check the [Locker.LastErr]() function to see if an error has occurred when WithNoPanic is specified.
func WithNoPanic() func(*Options) {
	return func(o *Options) {
		o.noPanic = true
	}
}

// WithDelay will delay the acquisition of the lock if it fails to acquire the lock. This is similar to the N option of setlock.
// The default is delay enabled (true). Specify false if you want to exit immediately if Lock acquisition fails.
func WithDelay(delay bool) func(*Options) {
	return func(o *Options) {
		o.delay = delay
	}
}

// WithExpireGracePeriod specifies the grace period after the lease expires
// during which the lock can still be reclaimed.
//
// If the grace period is greater than zero, the lock may be forcibly
// reacquired once both the lease has expired and the specified grace
// period has elapsed.
//
// If the grace period is zero or negative, automatic reclamation is disabled;
// expired locks will remain until removed by S3's TTL mechanism.
func WithExpireGracePeriod(d time.Duration) func(*Options) {
	return func(o *Options) {
		o.expireGracePeriod = d
	}
}
