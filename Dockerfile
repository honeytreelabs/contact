FROM docker.io/library/golang:1.26-alpine3.24 AS golang

WORKDIR /build
COPY . .

ENV CGO_ENABLED=0
RUN go build cmd/contact/contact.go

FROM docker.io/library/alpine:3.24 AS production
RUN addgroup -S -g 10001 contact && adduser -S -D -H -h /nonexistent -s /sbin/nologin -G contact -u 10001 contact
COPY --from=golang /build/contact /contact
USER contact:contact

CMD ["/contact"]
