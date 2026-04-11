# sets3lock

[![Go Reference](https://pkg.go.dev/badge/github.com/shogo82148/sets3lock.svg)](https://pkg.go.dev/github.com/shogo82148/sets3lock)

Distributed locking using Amazon S3.

`sets3lock` provides a distributed locking mechanism backed by Amazon S3.
It allows you to implement mutual exclusion across multiple processes or servers easily
and cost-effectively using only S3,
without the need to manage additional infrastructure like Redis or ZooKeeper.

## INSTALL

```bash
# use sets3lock via CLI.
go get github.com/shogo82148/cmd/sets3lock

# use sets3lock as a library.
go install github.com/shogo82148/sets3lock@latest
```

## USAGE

```plain
$ sets3lock [-nNxX] s3://bucket/key program [args...]
-n: No delay. If fn is locked by another process, sets3lock gives up.
-N: (Default.) Delay. If fn is locked by another process, sets3lock waits until it can obtain a new lock.
-x: If lock object creation/update fails or the lock cannot be obtained, sets3lock exits zero.
-X: (Default.) If lock object creation/update fails or the lock cannot be obtained, sets3lock prints an error message and exits nonzero.
-version: show version
-expire-grace-period: set expire grace period duration after TTL expiration
```

```go
import "github.com/shogo82148/sets3lock"

func main() {
  ctx := context.Background()
  l, err := sets3lock.New(ctx, "s3://bucket/key")
  if err != nil {
    panic(err)
  }
  if _, err := l.LockWithErr(ctx); err != nil {
    panic(err)
  }
  defer func() {
    if err := l.UnlockWithErr(ctx); err != nil {
      panic(err)
    }
  }()

  // do something that requires mutual exclusion.
}
```

You need to allow `s3:GetObject`, `s3:PutObject`, `s3:DeleteObject` actions by your IAM policy.

```json
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Effect": "Allow",
      "Action": ["s3:GetObject", "s3:PutObject", "s3:DeleteObject"],
      "Resource": "arn:aws:s3:::bucket/key"
    }
  ]
}
```

## RELATED WORKS

- [The setlock program](https://cr.yp.to/daemontools/setlock.html) (an utility of daemontools)
- [moznion/go-setlock](https://github.com/moznion/go-setlock)
- [fujiwara/go-redis-setlock](https://github.com/fujiwara/go-redis-setlock)
- [mashiike/setddblock](https://github.com/mashiike/setddblock)
