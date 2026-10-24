.PHONY: dev web build test fmt image clean

# Backend with the local dev login on :8080; run `make web` in a second terminal
# for the Vite dev server with hot reload on :5173.
dev:
	@mkdir -p web/dist && touch web/dist/.gitkeep
	WARRANTEED_DEV_LOGIN=true WARRANTEED_DATA_DIR=./data WARRANTEED_APP_URL=http://localhost:5173 go run ./cmd/warranteed

web:
	cd web && npm install && npm run dev

build:
	cd web && npm ci && npm run build
	@touch web/dist/.gitkeep
	CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o warranteed ./cmd/warranteed

test:
	@mkdir -p web/dist && touch web/dist/.gitkeep
	go vet ./...
	go test ./...
	cd web && npm run check

# Format and auto-fix everything.
fmt:
	gofmt -w .
	cd web && npm run lint:fix && npm run format

image:
	docker build -t warranteed:local .

clean:
	rm -rf warranteed web/dist/assets web/dist/index.html
