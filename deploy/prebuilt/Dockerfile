# Minimal image for `make release-prod`: wraps a PRE-BUILT linux/amd64 binary
# (cross-compiled natively on the host) so the image build is just a file copy.
# This avoids running bun/go under qemu emulation, which crashes (bun SIGILL)
# when building the full Dockerfile.wallfacerd on an arm64 host. CI still uses
# Dockerfile.wallfacerd (native linux runners); this is the local-deploy path.
FROM alpine:3.21
RUN apk add --no-cache ca-certificates
WORKDIR /app
COPY wallfacer .
EXPOSE 8080
ENTRYPOINT ["./wallfacer", "web"]
CMD ["-addr", ":8080"]
