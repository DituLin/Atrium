# End-to-end smoke (H-406)

Drives the real server and the real web client in headless Chrome: pairing page → CLI approval → dashboard with a slideshow image → `navigate`, `show`, `refresh` commands acknowledged as `applied` → remote-control key opens the collection browser.

Prerequisites: Google Chrome installed (the script launches Chrome via Playwright's `channel: 'chrome'`, so no browser download is needed), a built binary (`make web && make build`), Node 20+.

```sh
# 1. a throwaway server with TLS off and a local sample source
DIR=$(mktemp -d)
./bin/atrium init --config "$DIR/config.yaml" --data-dir "$DIR/data" --public-url http://127.0.0.1:18443
#    edit $DIR/config.yaml: server.listen 127.0.0.1:18443, tls.mode off, one source with identity.allow_local: true
./bin/atrium serve --config "$DIR/config.yaml" &

# 2. run the smoke
cd scripts/e2e && npm install && ATRIUM_CFG="$DIR/config.yaml" npm run smoke
```

Screenshots land in `scripts/e2e/out/`. Exit code 0 means every step passed; 2 means the page logged unexpected browser errors.
