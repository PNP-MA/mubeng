package mubeng

import (
	"net/http"
	"net/url"
	"sync"
	"testing"

	"github.com/go-test/deep"
)

func TestProxyNew(t *testing.T) {
	type fields struct {
		Address   string
		Transport *http.Transport
	}

	type args struct {
		req *http.Request
	}

	address := "http://localhost:3128"

	proxyURL, err := url.Parse(address)
	if err != nil {
		t.Fatal(err)
	}

	tr := &http.Transport{
		Proxy: http.ProxyURL(proxyURL),
	}

	req, err := http.NewRequest("GET", "http://localhost", nil)
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name   string
		fields fields
		args   args
		want   *http.Client
		want1  *http.Request
	}{
		{
			name: "New Proxy",
			fields: fields{
				Address:   address,
				Transport: tr,
			},
			args: args{
				req: req,
			},
			want: &http.Client{
				Transport: tr,
			},
			want1: req,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			proxy := &Proxy{
				Address:   tt.fields.Address,
				Transport: tt.fields.Transport,
			}
			got, got1 := proxy.New(tt.args.req)
			if diff := deep.Equal(got, tt.want); diff != nil {
				t.Error(diff)
			}
			if diff1 := deep.Equal(got1, tt.want1); diff1 != nil {
				t.Error(diff1)
			}
		})
	}
}

func TestProxyNewInvalidAddress(t *testing.T) {
	req, err := http.NewRequest("GET", "http://localhost", nil)
	if err != nil {
		t.Fatal(err)
	}

	proxy := &Proxy{
		Address:   "://bad",
		Transport: nil,
	}

	got, gotReq := proxy.New(req)

	if got == nil {
		t.Error("proxy.New() returned nil client")
	}
	if gotReq == nil {
		t.Error("proxy.New() returned nil request")
	}
}

func TestNoGlobalClientRace(t *testing.T) {
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			req, _ := http.NewRequest("GET", "http://localhost", nil)
			proxy := &Proxy{
				Address:   "http://localhost:3128",
				Transport: &http.Transport{},
			}
			_, _ = proxy.New(req)
		}()
	}
	wg.Wait()
}
