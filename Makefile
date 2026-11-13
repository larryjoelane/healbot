.PHONY: test race bench demo lint tidy

test:
	go test ./...

race:
	go test -race ./...

bench:
	go test -bench=. -benchtime=50ms -run='^$$' ./internal/ingest ./internal/group ./internal/store

demo:
	go run ./cmd/simulator

lint:
	go vet ./...

tidy:
	go mod tidy
