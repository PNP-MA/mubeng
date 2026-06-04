package server

import (
	"net/http"
	"sync"

	"github.com/henvic/httpretty"
	"github.com/mbndr/logo"
)

var (
	rotate  string
	handler *Proxy
	server  *http.Server
	client  *http.Client
	dump    *httpretty.Logger
	mime    = "text/plain"
	log     *logo.Logger
	ok      = 1

	mutex = sync.Mutex{}

	// dialTracker prevents multiple goroutines from dialing the same proxy
	// concurrently. Keys are proxy addresses; a loaded entry means another
	// goroutine is currently testing that proxy. Avoids burning N×the dial
	// timeout on a single dead proxy under concurrent load.
	dialTracker sync.Map
)
