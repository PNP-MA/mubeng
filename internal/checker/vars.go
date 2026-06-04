package checker

import (
	"sync"
)

var (
	myip   myIP
	wg     sync.WaitGroup

	endpoints = []string{
		"https://api.myip.com/",
		"https://ip-api.com/json/",
		"https://api.country.is/",
	}
	// endpoint is a backward-compatibility alias for endpoints[0].
	endpoint = endpoints[0]
)
