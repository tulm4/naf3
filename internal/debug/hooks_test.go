package debug

import (
	"context"
	"errors"
	"testing"
)

func TestWrapDB_NoOpWhenDisabled(t *testing.T) {
	d := &Debug{}
	called := false
	err := d.WrapDB(context.Background(), "pg.session.save", "sessions", func() error {
		called = true
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if !called {
		t.Fatal("fn was not called")
	}
}

func TestWrapDB_ReturnsOriginalError(t *testing.T) {
	d := &Debug{}
	want := errors.New("db down")
	got := d.WrapDB(context.Background(), "pg.x", "t", func() error { return want })
	if !errors.Is(got, want) {
		t.Fatalf("expected %v, got %v", want, got)
	}
}

func TestWrapRedis_NoOpWhenDisabled(t *testing.T) {
	d := &Debug{}
	called := false
	err := d.WrapRedis(context.Background(), "redis.x", "k", func() error {
		called = true
		return errors.New("ignored")
	})
	if err == nil {
		t.Fatal("expected original error to be returned")
	}
	if !called {
		t.Fatal("fn was not called")
	}
}

func TestWrapProtocol_NoOpWhenDisabled(t *testing.T) {
	d := &Debug{}
	called := false
	err := d.WrapProtocol(context.Background(), "aaa.radius.forward", func() error {
		called = true
		return nil
	})
	if err != nil || !called {
		t.Fatalf("err=%v called=%v", err, called)
	}
}
