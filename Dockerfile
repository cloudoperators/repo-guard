# Build the manager binary
FROM golang:1.25 as builder
ARG TARGETOS
ARG TARGETARCH

WORKDIR /workspace
# Copy the Go Modules manifests
COPY go.mod go.mod
COPY go.sum go.sum
# cache deps before building and copying source so that we don't need to re-download as much
# and so that source changes don't invalidate our downloaded layer
RUN go mod download

# Copy the go source
COPY cmd/main.go cmd/main.go
COPY api/ api/
COPY internal/ internal/

# Build
# the GOARCH has not a default value to allow the binary be built according to the host where the command
# was called. For example, if we call make docker-build in a local env which has the Apple Silicon M1 SO
# the docker BUILDPLATFORM arg will be linux/arm64 when for Apple x86 it will be linux/amd64. Therefore,
# by leaving it empty we can ensure that the container and binary shipped on it will have the same platform.
RUN CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH} go build -a -o manager cmd/main.go

## get SAP certificates
FROM alpine:3.23 AS certificate

RUN apk upgrade --no-cache --no-progress \
  && apk add --no-cache --no-progress ca-certificates

RUN wget http://aia.pki.co.sap.com/aia/SAP%20Global%20Root%20CA.crt -O /usr/local/share/ca-certificates/SSAP%20Global%20Root%20CA.crt && update-ca-certificates

# Use distroless as minimal base image to package the manager binary
# Refer to https://github.com/GoogleContainerTools/distroless for more details
FROM gcr.io/distroless/static:nonroot
LABEL source_repository="https://github.com/cloudoperators/repo-guard"
WORKDIR /
COPY --from=builder /workspace/manager .
USER 65532:65532

COPY --from=certificate /etc/ssl/certs /etc/ssl/certs

ENTRYPOINT ["/manager"]
