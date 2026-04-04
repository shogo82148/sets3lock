package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"syscall"
	"time"

	"github.com/shogo82148/sets3lock"
)

var (
	Version = "current"
)

var signals = []os.Signal{
	syscall.SIGHUP,
	syscall.SIGINT,
	syscall.SIGTERM,
	syscall.SIGQUIT,
}

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
	lockGranted, err := lock(ctx, locker)
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
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := locker.UnlockWithErr(ctx); err != nil {
			fmt.Fprintf(os.Stderr, "sets3lock: failed to release lock: %v\n", err)
		}
	}()

	cmd := exec.CommandContext(ctx, flag.Arg(1), flag.Args()[2:]...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		fmt.Fprintf(os.Stderr, "sets3lock: unable to start command: %v\n", err)
		return 1
	}
	cmdCh := make(chan error, 1)
	go func() {
		cmdCh <- cmd.Wait()
	}()

	signalCh := make(chan os.Signal, 1)
	signal.Notify(signalCh, signals...)
	defer signal.Stop(signalCh)

	for {
		select {
		case err := <-cmdCh:
			if code := cmd.ProcessState.ExitCode(); code != 0 {
				return code
			}
			if err != nil {
				fmt.Fprintf(os.Stderr, "sets3lock: command exited with error: %v\n", err)
				return 1
			}
			return 0
		case s := <-signalCh:
			if err := cmd.Process.Signal(s); err != nil {
				fmt.Fprintf(os.Stderr, "sets3lock: failed to forward signal: %v\n", err)
			}
		}
	}
}

func lock(ctx context.Context, locker *sets3lock.Locker) (bool, error) {
	ctx, cancel := signal.NotifyContext(ctx, signals...)
	defer cancel()
	return locker.LockWithErr(ctx)
}
