# go-cross

An example project to learn Go cross-compilation, using a minimal Kafka consumer
built on [confluent-kafka-go](https://github.com/confluentinc/confluent-kafka-go)
(librdkafka, cgo).

Pure Go cross-compiles with just `GOOS`/`GOARCH`. cgo needs a C toolchain for
the target, which is the hard part. This project compares ways to do it.

## Results

Host: darwin/arm64, 15 cores, 24 GB. Containers run in the Finch VM (8 cores,
12 GB). go1.26.0, full rebuilds (`-a`).

| Step | Target | How | Image (once) | Build (wall) |
|---|---|---|---|---|
| 1 | darwin/arm64 | native | - | 3.4 s |
| 1 | linux/arm64 | naive cross-compile | - | fails |
| 2 | linux/arm64 musl | native container | 1 min 52 s | 4.5 s |
| 3 | linux/amd64 musl | emulated container | 1 min 50 s | 52 s |
| 4 | linux/amd64 + arm64 musl | one multi-platform build | 5 min 26 s | 59 s |
| 5 | linux/amd64 + arm64 glibc | one multi-platform build, default gcc | 5 min 52 s | 54 s |
| 6 | linux/amd64 + arm64 musl | zig cross-compile, no emulation | 3 min 48 s | 22 s |
| 7 | linux/amd64 + arm64 glibc | GoReleaser in goreleaser-cross, cross gcc | 20 min 0 s | 5.6 s |
| 8 | - | run binaries on other distros | - | musl runs everywhere, glibc fails on old glibc |

Binaries are written to `bin/` (git-ignored): darwin 12 MB, Linux musl static
14 MB (gcc) or 17-18 MB (zig), Linux glibc dynamic 13-14 MB
(11-12 MB stripped, GoReleaser in `dist/`).

## Measurement rule

Each step starts cold. Before a container step, remove the project images and
the build cache:

```sh
finch rmi -f $(finch images -q --filter reference='go-cross*' | sort -u) 2>/dev/null
finch builder prune -a -f
```

Build times are wall-clock (`real`/`total`), measured around the compile only
(`go build`, the whole compile `finch build` in steps 4 to 6, `goreleaser build`
in step 7).
Image generation (toolchain + Go modules + base image pull) is measured
separately.

## Step 1: Native build, then naive cross-compilation

```sh
echo "platform: $(go env GOOS)/$(go env GOARCH)" && \
echo "cores: $(getconf _NPROCESSORS_ONLN)" && \
echo "memory: $(( $(sysctl -n hw.memsize) / 1024 / 1024 / 1024 )) GB" && \
time go build -a -o bin/go-cross-darwin-arm64 .
```

```
platform: darwin/arm64
cores: 15
memory: 24 GB
go build -a -o bin/go-cross-darwin-arm64 .  11,09s user 4,65s system 467% cpu 3,369 total
```

Natively, cgo uses the host `clang` and just works. Cross-compiling the same
code to linux/arm64 fails both ways, which is what the next steps solve.

Go disables cgo when cross-compiling, and the package is cgo-only:

```sh
GOOS=linux GOARCH=arm64 go build -o /dev/null .
```

```
kafka/deserialized_message.go:28:17: undefined: TopicPartition
```

Enabling cgo uses the host `clang`, which builds against macOS headers:

```sh
CGO_ENABLED=1 GOOS=linux GOARCH=arm64 go build -o /dev/null .
```

```
gcc_clearenv.c:16:2: error: call to undeclared function 'clearenv'
linux_syscall.c:67:13: error: call to undeclared function 'setresgid'
```

A C toolchain for the target is needed: the next steps use containers.

## Step 2: linux/arm64 musl in a container

The [static build docs](https://github.com/confluentinc/confluent-kafka-go#static-builds-on-linux)
use musl + `-tags musl` + static linking, which avoids glibc version mismatches.
They need a `musl-gcc`, which macOS doesn't provide (only third-party Homebrew
taps); on Alpine it is the system compiler. The host is arm64, so the container
runs natively.

`build/musl/Dockerfile` holds only dependencies. `.dockerignore` only sends
`go.mod`/`go.sum`, so code changes don't rebuild the image; sources are mounted.

```sh
time finch build --platform linux/arm64 -t go-cross-musl -f build/musl/Dockerfile .
# 1:51,74 total

finch run --rm --platform linux/arm64 -v "$PWD":/src -w /src go-cross-musl sh -c '
echo "platform: $(go env GOOS)/$(go env GOARCH)"
echo "cores: $(nproc)"
echo "memory: $(free -g | awk "/Mem:/{print \$2}") GB"
time go build -a -tags musl -ldflags "-linkmode external -extldflags -static" -o bin/go-cross-linux-arm64-musl .
'
# platform: linux/arm64, cores: 8, memory: 12 GB
# real 0m 4.54s  user 0m 13.10s  sys 0m 1.47s
```

## Step 3: linux/amd64 musl in an emulated container

Same Dockerfile with `--platform linux/amd64`. The VM emulates x86_64, so the
compiler itself runs emulated: a native amd64 build on an emulated machine, not
a cross-compilation. About 11x slower than step 2.

```sh
time finch build --platform linux/amd64 -t go-cross-musl-amd64 -f build/musl/Dockerfile .
# 1:49,50 total

finch run --rm --platform linux/amd64 -v "$PWD":/src -w /src go-cross-musl-amd64 sh -c '
echo "platform: $(go env GOOS)/$(go env GOARCH) (uname -m: $(uname -m))"
echo "cores: $(nproc)"
echo "memory: $(free -g | awk "/Mem:/{print \$2}") GB"
time go build -a -tags musl -ldflags "-linkmode external -extldflags -static" -o bin/go-cross-linux-amd64-musl .
'
# platform: linux/amd64 (uname -m: x86_64), cores: 8, memory: 12 GB
# real 0m 52.00s  user 3m 2.02s  sys 0m 15.11s
```

## Step 4: linux/amd64 + linux/arm64 in one build

`build/multi/Dockerfile` builds both platforms in parallel (arm64 native, amd64
emulated):

- `deps`: toolchain + Go modules only
- `build`: copies sources (`Dockerfile.dockerignore` also lets `*.go` through) and compiles
- final `scratch` stage: only the binary, exported with `--output`

```sh
time finch build --platform linux/amd64,linux/arm64 --target deps -t go-cross-multi-deps -f build/multi/Dockerfile .
# 5:25,87 total

time finch build --progress=plain --platform linux/amd64,linux/arm64 \
  --output type=local,dest=bin/multi -f build/multi/Dockerfile .
# [linux/arm64] real 0m 6.32s   user 0m 14.24s  sys 0m 1.56s
# [linux/amd64] real 0m 54.85s  user 3m 4.80s   sys 0m 14.75s
# 59,314 total
```

Binaries are identical (same BuildID) to steps 2 and 3. Total time is bounded by
the emulated amd64 build; both builds share the VM's 8 cores, so each is slower
than alone.

## Step 5: linux/amd64 + linux/arm64 with the default gcc (glibc)

Same as step 4, but on the Debian-based `golang:1.26` image, which already ships
gcc 14.2 and glibc: no `apk add`, no `-tags musl`, no static linking.
`build/glibc/Dockerfile`, same stages as step 4.

```sh
time finch build --platform linux/amd64,linux/arm64 --target deps -t go-cross-glibc-deps -f build/glibc/Dockerfile .
# 5:51,63 total

time finch build --progress=plain --platform linux/amd64,linux/arm64 \
  --output type=local,dest=bin/glibc -f build/glibc/Dockerfile .
# [linux/arm64] real 0m5.829s   user 0m12.975s  sys 0m1.515s
# [linux/amd64] real 0m52.336s  user 3m1.378s   sys 0m14.465s
# 54,001 total
```

```
$ file bin/glibc/*/go-cross
bin/glibc/linux_amd64/go-cross: ELF 64-bit LSB executable, x86-64, dynamically linked, interpreter /lib64/ld-linux-x86-64.so.2, ...
bin/glibc/linux_arm64/go-cross: ELF 64-bit LSB executable, ARM aarch64, dynamically linked, interpreter /lib/ld-linux-aarch64.so.1, ...
```

Compile times match step 4. The image is larger to pull (full Debian vs Alpine).

**Watch out for the glibc version.** glibc versions its symbols, and a binary
records the versions it was linked against on the build system. Newer glibc keeps
old versions (backward compatible), but an older one lacks new versions. So a
binary built on a newer system fails on an older target:

```
$ grep -ao 'GLIBC_[0-9][0-9.]*' bin/glibc/linux_amd64/go-cross | sort -uV | tail -1
GLIBC_2.34
```

```
$ ./go-cross   # on Debian 11 (glibc 2.31), see step 8
./go-cross: /lib/aarch64-linux-gnu/libc.so.6: version `GLIBC_2.34' not found (required by ./go-cross)
```

These binaries need glibc >= 2.34: they fail on e.g. Debian 11, Ubuntu 20.04,
RHEL 8 or Amazon Linux 2. Build on the oldest glibc you target, or use static
musl (steps 2-4, 6).

## Step 6: linux/amd64 + linux/arm64 cross-compiled with zig

Following [goreleaser/example-zig-cgo](https://github.com/goreleaser/example-zig-cgo):
`zig cc` is a C compiler that bundles libc (glibc, musl) for many targets, so
`CC="zig cc -target x86_64-linux-musl"` cross-compiles cgo from any host.

`build/zig/Dockerfile` runs every stage on the build platform
(`FROM --platform=$BUILDPLATFORM`), so both targets compile natively on arm64:
real cross-compilation, no emulation. Same stages as step 4, plus a warm-up in
`deps`: zig builds musl for each target on first use (~80 MB cache), which is a
dependency, not compilation.

```sh
time finch build --platform linux/amd64,linux/arm64 --target deps -t go-cross-zig-deps -f build/zig/Dockerfile .
# 3:47,56 total

time finch build --progress=plain --platform linux/amd64,linux/arm64 \
  --output type=local,dest=bin/zig -f build/zig/Dockerfile .
# [linux/arm64 -> linux/amd64] real 0m 19.78s  user 0m 41.24s  sys 0m 13.65s
# [linux/arm64 -> linux/arm64] real 0m 20.14s  user 0m 42.46s  sys 0m 14.65s
# 21,882 total
```

```
$ file bin/zig/*/go-cross
bin/zig/linux_amd64/go-cross: ELF 64-bit LSB executable, x86-64, ..., statically linked, ...
bin/zig/linux_arm64/go-cross: ELF 64-bit LSB executable, ARM aarch64, ..., statically linked, ...
```

amd64 drops from ~55 s (emulated, step 4) to ~20 s. arm64 is slower than gcc
(~6 s), even with musl already built in `deps`.

## Step 7: linux/amd64 + linux/arm64 with GoReleaser (goreleaser-cross)

[goreleaser-cross](https://github.com/goreleaser/goreleaser-cross) is a Docker
image with GoReleaser and cross toolchains: gnu gcc for Linux targets, mingw for
Windows, osxcross for macOS. `.goreleaser.yaml` sets `CC` per build
(`x86_64-linux-gnu-gcc`, `aarch64-linux-gnu-gcc`). The arm64 image runs natively
and cross-compiles amd64 with a real cross gcc: no emulation.

`build/goreleaser/Dockerfile` adds only the Go modules to the image (Ubuntu 24.04,
go1.26.5, GoReleaser 2.17.1). The image is ~2 GB compressed, hence the long
generation.

```sh
time finch build --platform linux/arm64 -t go-cross-goreleaser -f build/goreleaser/Dockerfile .
# 20:00,23 total

finch run --rm --platform linux/arm64 -v "$PWD":/src -w /src --entrypoint bash go-cross-goreleaser -c '
git config --global --add safe.directory /src
echo "platform: $(go env GOOS)/$(go env GOARCH) (uname -m: $(uname -m))"
echo "cores: $(nproc)"
echo "memory: $(free -g | awk "/Mem:/{print \$2}") GB"
time goreleaser build --snapshot --clean
'
# platform: linux/arm64 (uname -m: aarch64), cores: 8, memory: 11 GB
# building ... target=linux_arm64_v8.0
# building ... target=linux_amd64_v1
# real 0m5.555s  user 0m25.457s  sys 0m2.970s
```

```
$ file dist/*/go-cross
dist/linux-amd64_linux_amd64_v1/go-cross: ELF 64-bit LSB executable, x86-64, dynamically linked, interpreter /lib64/ld-linux-x86-64.so.2, ..., stripped
dist/linux-arm64_linux_arm64_v8.0/go-cross: ELF 64-bit LSB executable, ARM aarch64, dynamically linked, interpreter /lib/ld-linux-aarch64.so.1, ..., stripped
```

Fastest compile: real cross gcc for both targets, built in parallel. But the
binaries are glibc dynamic (need GLIBC_2.34, like step 5), and GoReleaser strips
them by default (`-s -w`).

## Step 8: Running the binaries on other systems

The static musl binaries (steps 2 and 6) embed their libc, so they only need a
Linux kernel. The glibc binaries (steps 5 and 7) load the target's glibc. The arm64
binaries, run on older glibc systems and on a musl-only system:

```sh
for img in debian:bullseye ubuntu:20.04 debian:bookworm alpine:3.20; do
  for b in bin/go-cross-linux-arm64-musl bin/zig/linux_arm64/go-cross \
           bin/glibc/linux_arm64/go-cross dist/linux-arm64_linux_arm64_v8.0/go-cross; do
    finch run --rm --platform linux/arm64 -v "$PWD":/src:ro $img \
      sh -c "ldd --version 2>&1 | head -1; timeout 3 /src/$b"
  done
done
```

| Run on | System libc | musl, gcc (step 2) | musl, zig (step 6) | glibc (step 5) | glibc, GoReleaser (step 7) |
|---|---|---|---|---|---|
| Debian 11 | glibc 2.31 | runs | runs | `GLIBC_2.32`, `GLIBC_2.34` not found | `GLIBC_2.32`, `GLIBC_2.34` not found |
| Ubuntu 20.04 | glibc 2.31 | runs | runs | `GLIBC_2.32`, `GLIBC_2.34` not found | `GLIBC_2.32`, `GLIBC_2.34` not found |
| Debian 12 | glibc 2.36 | runs | runs | runs | runs |
| Alpine 3.20 | musl | runs | runs | `No such file or directory` (no glibc loader) | `No such file or directory` (no glibc loader) |

"Runs" means the consumer starts and librdkafka tries the broker
(`Connection refused`, no Kafka running).

The zig linux/amd64 binary also runs on Debian 11 (emulated).

## References

- [confluent-kafka-go](https://github.com/confluentinc/confluent-kafka-go)
- [confluent-kafka-go: static builds on Linux](https://github.com/confluentinc/confluent-kafka-go#static-builds-on-linux)
- [goreleaser/example-zig-cgo](https://github.com/goreleaser/example-zig-cgo)
- [goreleaser/goreleaser-cross](https://github.com/goreleaser/goreleaser-cross)
