package server

import (
	"testing"
	"time"

	"github.com/fsnotify/fsnotify"
)

func TestWatchExitsOnClose(t *testing.T) {
	w, err := fsnotify.NewWatcher()
	if err != nil {
		t.Fatal(err)
	}

	done := make(chan struct{})
	go func() {
		watch(w)
		close(done)
	}()

	w.Close()

	select {
	case <-done:
		// goroutine exited cleanly
	case <-time.After(time.Second):
		t.Fatal("watch() did not exit after watcher.Close()")
	}
}
