FROM mcr.microsoft.com/devcontainers/go:1-1.24-bookworm

COPY go.mod /gomod/go.mod
COPY go.sum /gomod/go.sum

RUN cd /gomod && \
    GINKGO_VERSION="$(go list -m -f "{{ .Version }}" github.com/onsi/ginkgo/v2)" && \
    go install "github.com/onsi/ginkgo/v2/ginkgo@${GINKGO_VERSION}" && \
    cd / && \
    rm -rf /gomod && \
    chown -R vscode:vscode /go
