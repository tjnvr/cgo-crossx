# Multi-platform build: each --platform runs natively or emulated in the Finch VM.

# deps: toolchain + Go modules only, cached independently from sources.
FROM golang:1.26-alpine AS deps
RUN apk add --no-cache gcc musl-dev
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download

# build: compiles the sources.
FROM deps AS build
COPY *.go ./
RUN echo "platform: $(go env GOOS)/$(go env GOARCH) (uname -m: $(uname -m))" && \
    echo "cores: $(nproc)" && \
    echo "memory: $(free -g | awk '/Mem:/{print $2}') GB" && \
    time go build -a -tags musl -ldflags "-linkmode external -extldflags -static" -o /out/go-cross .

# export: only the binary, written to the host with --output.
FROM scratch
COPY --from=build /out/go-cross /
