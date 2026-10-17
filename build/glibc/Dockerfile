# Multi-platform glibc build: the Debian-based golang image ships the default gcc.

# deps: Go modules only (gcc and libc6-dev are already in the base image).
FROM golang:1.26 AS deps
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download

# build: compiles the sources, dynamically linked against glibc.
FROM deps AS build
COPY *.go ./
RUN echo "platform: $(go env GOOS)/$(go env GOARCH) (uname -m: $(uname -m))" && \
    echo "cores: $(nproc)" && \
    echo "memory: $(free -g | awk '/Mem:/{print $2}') GB" && \
    echo "gcc: $(gcc --version | head -1)" && \
    bash -c 'time go build -a -o /out/go-cross .'

# export: only the binary, written to the host with --output.
FROM scratch
COPY --from=build /out/go-cross /
