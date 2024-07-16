// Copyright 2018 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package h2c

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"golang.org/x/net/http2"
)

func ExampleNewHandler() {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "Hello world")
	})
	h2s := &http2.Server{
		// ...
	}
	h1s := &http.Server{
		Addr:    ":8080",
		Handler: NewHandler(handler, h2s),
	}
	log.Fatal(h1s.ListenAndServe())
}

func TestContext(t *testing.T) {
	baseCtx := context.WithValue(context.Background(), "testkey", "testvalue")

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.ProtoMajor != 2 {
			t.Errorf("Request wasn't handled by h2c.  Got ProtoMajor=%v", r.ProtoMajor)
		}
		if r.Context().Value("testkey") != "testvalue" {
			t.Errorf("Request doesn't have expected base context: %v", r.Context())
		}
		fmt.Fprint(w, "Hello world")
	})

	h2s := &http2.Server{}
	h1s := httptest.NewUnstartedServer(NewHandler(handler, h2s))
	h1s.Config.BaseContext = func(_ net.Listener) context.Context {
		return baseCtx
	}
	h1s.Start()
	defer h1s.Close()

	client := &http.Client{
		Transport: &http2.Transport{
			AllowHTTP: true,
			DialTLS: func(network, addr string, _ *tls.Config) (net.Conn, error) {
				return net.Dial(network, addr)
			},
		},
	}

	resp, err := client.Get(h1s.URL)
	if err != nil {
		t.Fatal(err)
	}
	_, err = ioutil.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if err := resp.Body.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestMaxBytesHandler(t *testing.T) {
	const bodyLimit = 10
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("got request, expected to be blocked by body limit")
	})

	h2s := &http2.Server{}
	h1s := httptest.NewUnstartedServer(http.MaxBytesHandler(NewHandler(handler, h2s), bodyLimit))
	h1s.Start()
	defer h1s.Close()

	// Wrap the body in a struct{io.Reader} to prevent it being rewound and resent.
	body := "0123456789abcdef"
	req, err := http.NewRequest("POST", h1s.URL, struct{ io.Reader }{strings.NewReader(body)})
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Http2-Settings", "")
	req.Header.Set("Upgrade", "h2c")
	req.Header.Set("Connection", "Upgrade, HTTP2-Settings")

	resp, err := h1s.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	_, err = ioutil.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := resp.StatusCode, http.StatusInternalServerError; got != want {
		t.Errorf("resp.StatusCode = %v, want %v", got, want)
	}
}
