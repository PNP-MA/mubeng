package server

import (
	"context"
	"os"
	"time"

	"github.com/fsnotify/fsnotify"
)

// Stop will terminate proxy server
func Stop(ctx context.Context) {
	_ = server.Shutdown(ctx)
}

func interrupt(sig chan os.Signal) {
	<-sig
	log.Warn("Interuppted. Exiting...")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	Stop(ctx)
}

func watch(w *fsnotify.Watcher) {
	for {
		select {
		case event, ok := <-w.Events:
			if !ok {
				return
			}
			if event.Op == 2 {
				log.Info("Proxy file has changed, reloading...")

				err := handler.Options.ProxyManager.Reload()
				if err != nil {
					log.Errorf("Reload failed: %s", err)
					continue
				}
			}
		case err, ok := <-w.Errors:
			if !ok {
				return
			}
			log.Errorf("Watcher error: %s", err)
			continue
		}
	}
}
