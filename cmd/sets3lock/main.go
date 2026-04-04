package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"time"

	"github.com/shogo82148/sets3lock"
)

var (
	Version = "current"
)

func main() {
	os.Exit(_main())
}

func _main() int {
	var (
		n, N, x, X, versionFlag bool
		expireGracePeriod       time.Duration
	)
	flag.BoolVar(&n, "n", false, "No delay. If fn is locked by another process, sets3lock gives up.")
	flag.BoolVar(&N, "N", false, "(Default.) Delay. If fn is locked by another process, sets3lock waits until it can obtain a new lock.")
	flag.BoolVar(&x, "x", false, "If fn cannot be update-item (or put-item) or locked, sets3lock exits zero.")
	flag.BoolVar(&X, "X", false, "(Default.) If fn cannot be update-item (or put-item) or locked, sets3lock prints an error message and exits nonzero.")
	flag.BoolVar(&versionFlag, "version", false, "show version")
	flag.DurationVar(&expireGracePeriod, "expire-grace-period", 0, "set expire grace period duration after TTL expiration")
	flag.Parse()

	if versionFlag {
		fmt.Fprintf(flag.CommandLine.Output(), "sets3lock version: %s\n", Version)
		fmt.Fprintf(flag.CommandLine.Output(), "go runtime version: %s\n", runtime.Version())
		return 0
	}
	if flag.NArg() < 1 {
		flag.CommandLine.Usage()
		fmt.Fprintf(flag.CommandLine.Output(), "\nsets3lock: missing s3 dsn\n")
		return 1
	}
	if flag.NArg() < 2 {
		flag.CommandLine.Usage()
		fmt.Fprintf(flag.CommandLine.Output(), "\nsets3lock: missing your command\n")
		return 1
	}

	// -N and -n both specified, Delay is true by default
	// -N and -n both not specified, Delay is true by default
	// -N specified, -n not specified, Delay is true
	// -N not specified, -n specified, Delay is false
	delay := N || (!N && !n)
	optFns := []func(*sets3lock.Options){
		sets3lock.WithDelay(delay),
	}
	if expireGracePeriod > 0 {
		optFns = append(optFns, sets3lock.WithExpireGracePeriod(expireGracePeriod))
	}
	ctx := context.Background()
	locker, err := sets3lock.New(ctx, flag.Arg(0), optFns...)
	if err != nil {
		fmt.Fprintf(os.Stderr, "sets3lock: failed to create locker: %v\n", err)
		return 1
	}
	lockGranted, err := locker.LockWithErr(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "sets3lock: failed to acquire lock: %v\n", err)
		return 1
	}
	if !lockGranted {
		fmt.Fprintf(os.Stderr, "sets3lock: lock was not granted\n")
		if x && !X {
			return 0
		}
		return 1
	}
	defer locker.Unlock()

	cmd := exec.CommandContext(ctx, flag.Arg(1), flag.Args()[2:]...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	err = cmd.Run()
	if err != nil {
		fmt.Fprintf(os.Stderr, "sets3lock: unable to run command: %v\n", err)
		return 1
	}
	return 0
}
