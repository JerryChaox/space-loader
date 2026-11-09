# space-loader

Prototype Telegram Mini App page that opens a "Space" (a web page on another machine's localhost) through a [tailcat](https://github.com/tailscale/tailcat) tunnel running in the browser as WebAssembly.

- `space.wasm` is built from `src/main_js.go` against tailcat commit e00cdde (`GOOS=js GOARCH=wasm go build`). tailcat is BSD-3-Clause; see `LICENSE-tailcat`.
- The page holds no environment data. The tailcat address comes from the Telegram `start_param` or `#addr=` in the URL.
- Traffic is relayed through tailcat's public DERP relays.
