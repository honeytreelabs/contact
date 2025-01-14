all: contact

.PHONY: contact
contact:
	go build cmd/contact/contact.go

.PHONY: podman
podman:
	podman build -t registry-rw.honeytreelabs.com/contact:latest .

deploy: podman
	podman push registry-rw.honeytreelabs.com/contact:latest

.PHONY: test
test:
	go test ./...

.PHONY: request
request:
	curl -v http://localhost:8080/contact -X POST --data-raw 'email=email%40test.example.com&message=This+is+a+testmessage!!%3F!%3F!+!%40%23%24%25%5E%26*(()_%2B%3D%7D%7C%5C%5D%60~&contact-dsgvo-checkbox=on'
