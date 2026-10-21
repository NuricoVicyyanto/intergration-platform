.PHONY: help build test fmt vet run-api run-worker tidy clean

help:
	@echo "build      - kompilasi kedua binary ke ./bin"
	@echo "test       - jalankan unit test"
	@echo "fmt        - gofmt semua kode"
	@echo "vet        - go vet"
	@echo "run-api    - jalankan control plane API"
	@echo "run-worker - jalankan worker"
	@echo "clean      - hapus ./bin"

build:
	go build -o bin/control-plane ./cmd/control-plane
	go build -o bin/provision ./cmd/provision
	go build -o bin/worker ./cmd/worker

test:
	go test ./...

fmt:
	gofmt -w .

vet:
	go vet ./...

run-api:
	go run ./cmd/control-plane

run-worker:
	go run ./cmd/worker

tidy:
	go mod tidy

clean:
	rm -rf bin
