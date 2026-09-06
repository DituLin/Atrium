// Stub module: keeps `go build ./...` from descending into web/node_modules,
// which contains Go sources. The client bundle is embedded from internal/webui.
module atrium.invalid/web

go 1.24
