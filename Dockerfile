# Build the manager binary
FROM docker.io/golang:1.24 AS builder
ARG TARGETOS
ARG TARGETARCH

WORKDIR /workspace

COPY go.mod go.sum ./
COPY cmd/main.go cmd/main.go
COPY api/ api/
COPY internal/ internal/

ENV GOCACHE=/go-cache
ENV GOMODCACHE=/gomod-cache

RUN --mount=type=cache,target=/gomod-cache \
	--mount=type=cache,target=/go-cache \
	CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH} go build -a -o manager cmd/main.go

FROM gcr.io/distroless/static:nonroot
WORKDIR /
COPY --from=builder /workspace/manager .
USER 65532:65532

ENTRYPOINT ["/manager"]
