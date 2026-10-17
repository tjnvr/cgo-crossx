# Cross-compilation with zig: every stage runs on the build platform (native,
# no emulation), and zig cc targets each --platform's musl.

# deps: Go toolchain + zig + Go modules only. zig builds musl and compiler-rt
# per target on first use: warm its cache here so it isn't counted as compilation.
FROM --platform=$BUILDPLATFORM golang:1.26-alpine AS deps
RUN apk add --no-cache zig
RUN echo 'int main(void) { return 0; }' > /tmp/warm.c && \
    for t in x86_64-linux-musl aarch64-linux-musl; do \
      zig cc -target $t -static /tmp/warm.c -o /tmp/warm; \
    done && \
    rm /tmp/warm.c /tmp/warm
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download

# build: cross-compiles the sources for TARGETARCH.
FROM deps AS build
ARG TARGETARCH
COPY *.go ./
RUN case "$TARGETARCH" in \
      amd64) ZIG_TARGET=x86_64-linux-musl ;; \
      arm64) ZIG_TARGET=aarch64-linux-musl ;; \
    esac && \
    echo "platform: $(go env GOOS)/$(go env GOARCH) -> linux/$TARGETARCH (uname -m: $(uname -m))" && \
    echo "cores: $(nproc)" && \
    echo "memory: $(free -g | awk '/Mem:/{print $2}') GB" && \
    echo "zig: $(zig version), target $ZIG_TARGET" && \
    time env CGO_ENABLED=1 GOOS=linux GOARCH=$TARGETARCH \
      CC="zig cc -target $ZIG_TARGET" CXX="zig c++ -target $ZIG_TARGET" \
      go build -a -tags musl -ldflags "-linkmode external -extldflags -static" -o /out/go-cross .

# export: only the binary, written to the host with --output.
FROM scratch
COPY --from=build /out/go-cross /
