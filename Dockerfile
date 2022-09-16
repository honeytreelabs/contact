FROM golang:1.19-alpine AS golang

WORKDIR /build
COPY . .

ENV CGO_ENABLED=0
RUN go build cmd/contact/contact.go

FROM alpine:3.16 AS production
COPY --from=golang /build/contact /contact

CMD ["/contact"]
