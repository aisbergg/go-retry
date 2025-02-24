package retry

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestRetryableError(t *testing.T) {
	t.Parallel()

	err := RetryableError(fmt.Errorf("oops"))
	if got, want := err.Error(), "retryable: "; !strings.Contains(got, want) {
		t.Errorf("expected %v to contain %v", got, want)
	}
}

func TestDo(t *testing.T) {
	t.Parallel()

	t.Run("exit_on_max_attempt", func(t *testing.T) {
		t.Parallel()

		ctx := context.Background()
		b := WithMaxRetries(3, BackoffFunc(func(err error) (time.Duration, error) {
			return 1 * time.Nanosecond, err
		}))

		var i int
		if err := Do(ctx, b, func(_ context.Context) error {
			i++
			return RetryableError(fmt.Errorf("oops"))
		}); err == nil {
			t.Fatal("expected err")
		}

		// 1 + retries
		if got, want := i, 4; got != want {
			t.Errorf("expected %v to be %v", got, want)
		}
	})

	t.Run("exit_on_non_retryable", func(t *testing.T) {
		t.Parallel()

		ctx := context.Background()
		b := WithRetryable(WithMaxRetries(3, BackoffFunc(func(err error) (time.Duration, error) {
			return 1 * time.Nanosecond, err
		})))

		var i int
		if err := Do(ctx, b, func(_ context.Context) error {
			i++
			return fmt.Errorf("oops") // not retryable
		}); err == nil {
			t.Fatal("expected err")
		}

		if got, want := i, 1; got != want {
			t.Errorf("expected %v to be %v", got, want)
		}
	})

	t.Run("unwraps", func(t *testing.T) {
		t.Parallel()

		ctx := context.Background()
		b := WithRetryable(WithMaxRetries(1, BackoffFunc(func(err error) (time.Duration, error) {
			return 1 * time.Nanosecond, err
		})))

		err := Do(ctx, b, func(_ context.Context) error {
			return RetryableError(io.EOF)
		})
		if err == nil {
			t.Fatal("expected err")
		}

		if got, want := err, io.EOF; got != want {
			t.Errorf("expected %#v to be %#v", got, want)
		}
	})

	t.Run("exit_no_error", func(t *testing.T) {
		t.Parallel()

		ctx := context.Background()
		b := WithMaxRetries(3, BackoffFunc(func(err error) (time.Duration, error) {
			return 1 * time.Nanosecond, err
		}))

		var i int
		if err := Do(ctx, b, func(_ context.Context) error {
			i++
			return nil // no error
		}); err != nil {
			t.Fatal("expected no err")
		}

		if got, want := i, 1; got != want {
			t.Errorf("expected %v to be %v", got, want)
		}
	})

	t.Run("context_canceled", func(t *testing.T) {
		t.Parallel()

		b := BackoffFunc(func(err error) (time.Duration, error) {
			return 5 * time.Second, err
		})

		ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
		defer cancel()

		if err := Do(ctx, b, func(_ context.Context) error {
			return RetryableError(fmt.Errorf("oops")) // no error
		}); err != context.DeadlineExceeded {
			t.Errorf("expected %v to be %v", err, context.DeadlineExceeded)
		}
	})
}

func ExampleDo_simple() {
	ctx := context.Background()

	b := NewFibonacci(1 * time.Nanosecond)

	i := 0
	if err := Do(ctx, WithMaxRetries(3, b), func(ctx context.Context) error {
		fmt.Printf("%d\n", i)
		i++
		return RetryableError(fmt.Errorf("oops"))
	}); err != nil {
		// handle error
	}

	// Output:
	// 0
	// 1
	// 2
	// 3
}

func ExampleDo_customRetry() {
	ctx := context.Background()

	b := NewFibonacci(1 * time.Nanosecond)

	// This example demonstrates selectively retrying specific errors. Only errors
	// wrapped with RetryableError are eligible to be retried.
	if err := Do(ctx, WithMaxRetries(3, b), func(ctx context.Context) error {
		resp, err := http.Get("https://google.com/")
		if err != nil {
			return err
		}
		defer resp.Body.Close()

		switch resp.StatusCode / 100 {
		case 4:
			return fmt.Errorf("bad response: %v", resp.StatusCode)
		case 5:
			return RetryableError(fmt.Errorf("bad response: %v", resp.StatusCode))
		default:
			return nil
		}
	}); err != nil {
		// handle error
	}
}

func TestCancel(t *testing.T) {
	for i := 0; i < 100000; i++ {
		ctx, cancel := context.WithCancel(context.Background())

		calls := 0
		rf := func(ctx context.Context) error {
			calls++
			// Never succeed.
			// Always return a RetryableError
			return RetryableError(errors.New("nope"))
		}

		const delay time.Duration = time.Millisecond
		b := NewConstant(delay)

		const maxRetries = 5
		b = WithMaxRetries(maxRetries, b)

		const jitter time.Duration = 5 * time.Millisecond
		b = WithJitter(jitter, false, b)

		// Here we cancel the Context *before* the call to Do
		cancel()
		Do(ctx, b, rf)

		if calls > 1 {
			t.Errorf("rf was called %d times instead of 0 or 1", calls)
		}
	}
}
