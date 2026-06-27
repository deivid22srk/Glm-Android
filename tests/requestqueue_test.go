package tests

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"glm5.2proxy/internal/requestqueue"
)

func TestRequestQueueSerializesSameKey(t *testing.T) {
	queue := requestqueue.New()
	key := requestqueue.Key("account-one", "glm-5.2")
	var active int32
	var maxActive int32
	var wait sync.WaitGroup

	for range 3 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			lease, err := queue.Acquire(context.Background(), key)
			if err != nil {
				t.Errorf("acquire failed: %v", err)
				return
			}
			defer lease.Release()
			current := atomic.AddInt32(&active, 1)
			for {
				previous := atomic.LoadInt32(&maxActive)
				if current <= previous || atomic.CompareAndSwapInt32(&maxActive, previous, current) {
					break
				}
			}
			time.Sleep(20 * time.Millisecond)
			atomic.AddInt32(&active, -1)
		}()
	}

	wait.Wait()
	if maxActive != 1 {
		t.Fatalf("expected one active request per key, got %d", maxActive)
	}
	if items := queue.Snapshot(); len(items) != 0 {
		t.Fatalf("expected empty queue after releases, got %+v", items)
	}
}

func TestRequestQueueAllowsDifferentKeys(t *testing.T) {
	queue := requestqueue.New()
	var active int32
	var maxActive int32
	start := make(chan struct{})
	var wait sync.WaitGroup

	for _, key := range []string{requestqueue.Key("one", "glm-5.2"), requestqueue.Key("two", "glm-5.2")} {
		wait.Add(1)
		go func(key string) {
			defer wait.Done()
			lease, err := queue.Acquire(context.Background(), key)
			if err != nil {
				t.Errorf("acquire failed: %v", err)
				return
			}
			defer lease.Release()
			<-start
			current := atomic.AddInt32(&active, 1)
			for {
				previous := atomic.LoadInt32(&maxActive)
				if current <= previous || atomic.CompareAndSwapInt32(&maxActive, previous, current) {
					break
				}
			}
			time.Sleep(20 * time.Millisecond)
			atomic.AddInt32(&active, -1)
		}(key)
	}

	close(start)
	wait.Wait()
	if maxActive != 2 {
		t.Fatalf("expected different keys to run concurrently, got %d", maxActive)
	}
}

func TestRequestQueueBusyReflectsActiveLease(t *testing.T) {
	queue := requestqueue.New()
	key := requestqueue.Key("account-one", "glm-5.2")
	if queue.Busy(key) {
		t.Fatal("expected fresh queue key to be idle")
	}

	lease, err := queue.Acquire(context.Background(), key)
	if err != nil {
		t.Fatalf("acquire failed: %v", err)
	}
	if !queue.Busy(key) {
		t.Fatal("expected queue key to be busy while lease is active")
	}

	lease.Release()
	if queue.Busy(key) {
		t.Fatal("expected queue key to be idle after lease release")
	}
}
