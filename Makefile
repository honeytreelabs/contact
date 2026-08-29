IMAGE   ?= registry-rw.honeytreelabs.com/contact
TAG     ?= v1.8.0
GIT_COMMIT ?= $(shell git rev-parse --short=12 HEAD 2>/dev/null || echo unknown)
CONTAINER ?= podman
GO_TEST := go test
GOFMT_FILES := cmd/contact/contact.go cmd/contact/contact_test.go

ifeq ($(VERBOSE),1)
GO_TEST += -v
endif

all: build

## native targets

.PHONY: run
run:
	go build cmd/contact/contact.go

.PHONY: request
request:
	curl -v http://localhost:8080/contact -X POST --data-raw 'email=email%40test.example.com&message=This+is+a+testmessage!!%3F!%3F!+!%40%23%24%25%5E%26*(()_%2B%3D%7D%7C%5C%5D%60~&contact-dsgvo-checkbox=on'

.PHONY: test
test:
	$(GO_TEST) ./...

.PHONY: check
check:
	go mod verify
	test -z "$$(gofmt -l $(GOFMT_FILES))"
	$(GO_TEST) ./...

.PHONY: format
format:
	gofmt -w $(GOFMT_FILES)

.PHONY: audit
audit:
	go run golang.org/x/vuln/cmd/govulncheck@latest ./...

## container targets

.PHONY: build release push
build:
	$(CONTAINER) build --build-arg GIT_COMMIT=$(GIT_COMMIT) -t $(IMAGE):$(TAG) .

release: build
	$(CONTAINER) tag $(IMAGE):$(TAG) $(IMAGE):latest

push: release
	$(CONTAINER) push $(IMAGE):latest
	$(CONTAINER) push $(IMAGE):$(TAG)
