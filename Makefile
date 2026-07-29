IMAGE   ?= registry-rw.honeytreelabs.com/contact
TAG     ?= v1.6.0
GIT_COMMIT ?= $(shell git rev-parse --short=12 HEAD 2>/dev/null || echo unknown)
GO_TEST := go test

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

.PHONY: format
format:
	gofmt -w cmd/contact/contact.go cmd/contact/contact_test.go

## container targets

.PHONY: build release push
build:
	podman build --build-arg GIT_COMMIT=$(GIT_COMMIT) -t $(IMAGE):$(TAG) .

release: build
	podman tag $(IMAGE):$(TAG) $(IMAGE):latest

push: release
	podman push $(IMAGE):latest
	podman push $(IMAGE):$(TAG)
