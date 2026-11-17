package store_test

import (
	"context"
	"testing"
	"time"

	"github.com/statik/site-reliability-clanker/internal/store"
	"github.com/statik/site-reliability-clanker/internal/store/storetest"
)

func TestASecondWriterWaitsUntilTheFirstStops(t *testing.T) {
	url := storetest.URL(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	first, err := store.HoldWriter(ctx, url, func() { t.Error("the first writer waited") })
	if err != nil {
		t.Fatal(err)
	}
	waiting := make(chan struct{})
	held := make(chan *store.Writer)
	go func() {
		second, err := store.HoldWriter(ctx, url, func() { close(waiting) })
		if err != nil {
			t.Errorf("the second writer: %v", err)
		}
		held <- second
	}()
	select {
	case <-waiting:
	case <-held:
		t.Fatal("two writers held the lock at the same time")
	case <-ctx.Done():
		t.Fatal("the second writer did not report that it waits")
	}
	select {
	case <-held:
		t.Fatal("the second writer got the lock while the first held it")
	case <-time.After(300 * time.Millisecond):
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case second := <-held:
		if second != nil {
			if err := second.Close(); err != nil {
				t.Fatal(err)
			}
		}
	case <-ctx.Done():
		t.Fatal("the second writer did not get the lock after the first stopped")
	}
}

func TestAWaitingWriterStopsWithItsContext(t *testing.T) {
	url := storetest.URL(t)
	first, err := store.HoldWriter(context.Background(), url, func() {})
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	if _, err := store.HoldWriter(ctx, url, func() {}); err == nil {
		t.Fatal("a writer got the lock that another writer holds")
	}
}
