FROM golang:1.23-alpine AS golang

WORKDIR /build
COPY . .

ENV CGO_ENABLED=0
RUN go build cmd/contact/contact.go

FROM alpine:3.21 AS production
COPY --from=golang /build/contact /contact

CMD ["/contact"]
