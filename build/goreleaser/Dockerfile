# goreleaser-cross ships GoReleaser + cross toolchains (gnu gcc, mingw, osxcross).
# deps: Go modules only. Sources are mounted at run time.
FROM ghcr.io/goreleaser/goreleaser-cross:v1.26.5
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
