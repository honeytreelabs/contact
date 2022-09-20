all: contact

.PHONY: contact
contact:
	go build cmd/contact/contact.go

.PHONY: test
test:
	go test ./...
