.PHONY: build run test lint vet docker-build clean docker-start

BINARY_NAME = anon_test_data_generator
CMD_DIR     = ./cmd

build:
	go build -o $(BINARY_NAME) $(CMD_DIR)

run: build
	./$(BINARY_NAME)

test:
	go test -race -count=1 ./...

vet:
	go vet ./...

lint: vet
	@if command -v golangci-lint >/dev/null 2>&1; then \
		golangci-lint run ./...; \
	else \
		echo "golangci-lint not installed, skipping (go vet passed)"; \
	fi

docker-build:
	docker build -t anon_test_data_generator .

clean:
	rm -f $(BINARY_NAME)

docker-start:
	docker-compose up --build
