FROM golang:1.25-alpine AS builder

WORKDIR /build
COPY go.mod go.sum* ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /notebridge ./cmd/notebridge/

FROM alpine:3.20

RUN apk add --no-cache ca-certificates tzdata
COPY --from=builder /notebridge /usr/local/bin/notebridge

EXPOSE 19072
ENTRYPOINT ["notebridge"]
