package sets3lock

// Options are for changing the behavior of the lock mechanism.
type Options struct {
	client APIClient
}

func newOptions() *Options {
	return &Options{}
}

// WithAPIClient allows you to specify a custom S3 client for the locker.
func WithAPIClient(client APIClient) func(*Options) {
	return func(o *Options) {
		o.client = client
	}
}
