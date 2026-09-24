package sig

import (
	"context"
	"errors"
	"testing"
	"time"
)

var _ FlagsSpec = &Flags{}

type FlagsSpec interface {
	Set(flag ...string)
	Clear(flag ...string)
	IsSet(flag string) bool
	Flags() []string
	Wait(flag string, state bool) <-chan struct{}
	WaitContext(ctx context.Context, flag string, state bool) error
}

const TestFlag1 = "test1"
const TestFlag2 = "test2"

func TestWait(t *testing.T) {
	var flags = NewFlags()
	var tick = make(chan struct{})

	go func() {
		select {
		case <-flags.Wait(TestFlag1, false):
		case <-time.After(100 * time.Millisecond):
			t.Fatal("timeout reached")
		}

		tick <- struct{}{}

		select {
		case <-flags.Wait(TestFlag1, true):
		case <-time.After(100 * time.Millisecond):
			t.Fatal("timeout reached")
		}

		select {
		case <-flags.Wait(TestFlag2, true):
		case <-time.After(100 * time.Millisecond):
			t.Fatal("timeout reached")
		}

		tick <- struct{}{}

		select {
		case <-flags.Wait(TestFlag2, false):
		case <-time.After(100 * time.Millisecond):
			t.Fatal("timeout reached")
		}

		tick <- struct{}{}
	}()

	<-tick
	time.After(10 * time.Millisecond)

	flags.Set(TestFlag1, TestFlag2)

	<-tick
	time.After(10 * time.Millisecond)

	flags.Clear(TestFlag2)

	<-tick
}

func TestWaitContext(t *testing.T) {
	var flags = NewFlags()
	var tick = make(chan struct{})

	go func() {
		var err error

		ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
		defer cancel()

		err = flags.WaitContext(ctx, TestFlag1, true)
		if err != nil {
			t.Fatal("unexpected err:", err)
		}

		tick <- struct{}{}

		err = flags.WaitContext(ctx, TestFlag1, false)
		if err != nil {
			t.Fatal("unexpected err:", err)
		}

		err = flags.WaitContext(ctx, TestFlag2, false)
		if err != nil {
			t.Fatal("unexpected err:", err)
		}

		ctx, cancel = context.WithCancel(context.Background())
		cancel()

		err = flags.WaitContext(ctx, TestFlag2, true)
		if !errors.Is(err, context.Canceled) {
			t.Fatal("unexpected err:", err)
		}

		tick <- struct{}{}
	}()

	flags.IsSet(TestFlag1)

	<-tick

	flags.Clear(TestFlag1)

	<-tick
}
