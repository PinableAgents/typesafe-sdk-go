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

.PHONY: quality coverage examples
quality:
	go vet ./...
	go build ./...
	python3 -m unittest discover -s scripts -p 'test_check_*.py' -v
	$(MAKE) coverage

coverage:
	mkdir -p qa-results
	go test -race -count=1 -shuffle=on -coverpkg=./... -covermode=atomic -coverprofile=qa-results/coverage.out ./...
	go tool cover -func=qa-results/coverage.out
	python3 scripts/check_coverage.py qa-results/coverage.out --output qa-results/coverage-summary.json

examples:
	go test -count=1 -run '^Example' .
	go run ./examples/basic --mock
	go run ./examples/models --mock
	go run ./examples/agent --mock
