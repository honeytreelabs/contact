GIT_TAG := $(shell git describe --tag)

ifeq ($(GIT_TAG),)
  $(error GIT_TAG is empty. Please ensure you are in a Git repository with tags.)
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
	go test ./...

## container targets

.PHONY: build
build:
	podman build -t registry-rw.honeytreelabs.com/contact .

release: build
	podman tag registry-rw.honeytreelabs.com/contact registry-rw.honeytreelabs.com/contact:latest
	podman tag registry-rw.honeytreelabs.com/contact registry-rw.honeytreelabs.com/contact:$(GIT_TAG)

deploy: release
	podman push registry-rw.honeytreelabs.com/contact:latest
	podman push registry-rw.honeytreelabs.com/contact:$(GIT_TAG)
