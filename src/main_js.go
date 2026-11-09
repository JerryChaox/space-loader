// Space loader: tailcat in the browser, keeping one tunnel open and
// exposing HTTP over it to the page.
//
//	spaceConnect({addr, port, derpMapURL, verbose}) -> Promise<void>
//	spaceFetch({method, path, headers: [[k,v],...], body: Uint8Array|null})
//	    -> Promise<{status, statusText, headers: [[k,v],...], body: Uint8Array}>
package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"sync"
	"syscall/js"
	"time"

	"github.com/tailscale/tailcat"
	"tailscale.com/types/key"
	"tailscale.com/types/logger"
)

var (
	mu   sync.Mutex
	cl   *tailcat.Client
	hc   *http.Client
	port uint16
)

func main() {
	js.Global().Set("spaceConnect", js.FuncOf(spaceConnect))
	js.Global().Set("spaceFetch", js.FuncOf(spaceFetch))
	if f := js.Global().Get("onSpaceReady"); f.Type() == js.TypeFunction {
		f.Invoke()
	}
	select {}
}

func spaceConnect(this js.Value, args []js.Value) any {
	opts := args[0]
	addr := optString(opts, "addr")
	derpMapURL := optString(opts, "derpMapURL")
	p := uint16(80)
	if v := opts.Get("port"); v.Type() == js.TypeNumber {
		p = uint16(v.Int())
	}
	var logf logger.Logf = logger.Discard
	if opts.Get("verbose").Truthy() {
		logf = log.Printf
	}
	return makePromise(func() (any, error) {
		if addr == "" {
			return nil, errors.New("addr is required")
		}
		c := &tailcat.Client{Server: tailcat.Addr(addr), Key: key.NewNode(), Logf: logf, DERPMapURL: derpMapURL}
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		start := time.Now()
		for {
			pctx, pc := context.WithTimeout(ctx, 5*time.Second)
			_, err := c.Ping(pctx)
			pc()
			if err == nil {
				break
			}
			if ctx.Err() != nil {
				c.Close()
				return nil, fmt.Errorf("ping: %w", err)
			}
		}
		tr := &http.Transport{
			// A custom DialContext makes net/http use its own HTTP/1.1
			// stack instead of the browser's fetch.
			DialContext: func(ctx context.Context, network, a string) (net.Conn, error) {
				return c.DialTCPPort(ctx, p)
			},
			MaxIdleConnsPerHost: 6,
			IdleConnTimeout:     60 * time.Second,
		}
		mu.Lock()
		if cl != nil {
			cl.Close()
		}
		cl, port = c, p
		hc = &http.Client{
			Transport:     tr,
			Timeout:       60 * time.Second,
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		}
		mu.Unlock()
		return map[string]any{"connectMs": time.Since(start).Milliseconds()}, nil
	})
}

func spaceFetch(this js.Value, args []js.Value) any {
	opts := args[0]
	method := optString(opts, "method")
	path := optString(opts, "path")
	var hdrs [][2]string
	if h := opts.Get("headers"); h.Type() == js.TypeObject {
		for i := 0; i < h.Length(); i++ {
			hdrs = append(hdrs, [2]string{h.Index(i).Index(0).String(), h.Index(i).Index(1).String()})
		}
	}
	var body []byte
	if b := opts.Get("body"); b.Type() == js.TypeObject {
		body = make([]byte, b.Get("length").Int())
		js.CopyBytesToGo(body, b)
	}
	return makePromise(func() (any, error) {
		mu.Lock()
		c := hc
		mu.Unlock()
		if c == nil {
			return nil, errors.New("not connected")
		}
		if method == "" {
			method = "GET"
		}
		var rd io.Reader
		if body != nil {
			rd = bytes.NewReader(body)
		}
		req, err := http.NewRequest(method, "http://space"+path, rd)
		if err != nil {
			return nil, err
		}
		for _, kv := range hdrs {
			req.Header.Add(kv[0], kv[1])
		}
		resp, err := c.Do(req)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()
		data, err := io.ReadAll(resp.Body)
		if err != nil {
			return nil, err
		}
		var outH []any
		for k, vs := range resp.Header {
			for _, v := range vs {
				outH = append(outH, []any{k, v})
			}
		}
		u8 := js.Global().Get("Uint8Array").New(len(data))
		js.CopyBytesToJS(u8, data)
		return map[string]any{
			"status":     resp.StatusCode,
			"statusText": http.StatusText(resp.StatusCode),
			"headers":    outH,
			"body":       u8,
		}, nil
	})
}

func optString(v js.Value, name string) string {
	if p := v.Get(name); p.Type() == js.TypeString {
		return p.String()
	}
	return ""
}

func makePromise(f func() (any, error)) js.Value {
	handler := js.FuncOf(func(this js.Value, args []js.Value) any {
		resolve, reject := args[0], args[1]
		go func() {
			if res, err := f(); err == nil {
				resolve.Invoke(js.ValueOf(res))
			} else {
				reject.Invoke(js.Global().Get("Error").New(err.Error()))
			}
		}()
		return nil
	})
	return js.Global().Get("Promise").New(handler)
}
