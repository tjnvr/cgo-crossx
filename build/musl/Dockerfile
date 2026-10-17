# Build environment only: toolchain + Go modules. Sources are mounted at run time
# so that compilation is measured separately from image generation.
FROM golang:1.26-alpine

RUN apk add --no-cache gcc musl-dev

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
