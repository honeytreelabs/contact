FROM docker.io/library/golang:1.26-alpine3.24 AS golang

WORKDIR /build
COPY . .

ENV CGO_ENABLED=0
RUN go build cmd/contact/contact.go

FROM docker.io/library/alpine:3.24 AS production
COPY --from=golang /build/contact /contact

CMD ["/contact"]
