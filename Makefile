.PHONY: test race vet mock check

test:
	go test ./...

race:
	go test -race ./...

vet:
	go vet ./...

mock:
	go run ./examples/basic -mock
	go run ./examples/models -mock
	go run ./examples/agent -mock

check: vet test race mock
