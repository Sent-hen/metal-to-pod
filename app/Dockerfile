# syntax=docker/dockerfile:1
#
# Multi-stage build. The pipeline runs `--target test` first as a gate, then
# builds the default (final) target. Only the static binary ships: the runtime
# image has no shell, package manager or libc - a small attack surface and a
# ~10 MB image that pulls fast onto freshly joined nodes.

FROM golang:1.26-alpine AS base
WORKDIR /src
COPY go.mod ./
RUN go mod download
COPY . .

FROM base AS test
RUN go vet ./... && CGO_ENABLED=0 go test ./...

FROM base AS build
ARG VERSION=dev
ARG COMMIT=unknown
RUN CGO_ENABLED=0 go build -trimpath \
      -ldflags "-s -w -X main.version=${VERSION} -X main.commit=${COMMIT}" \
      -o /out/server ./cmd/server

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/server /server
USER 65532:65532
EXPOSE 8080
ENTRYPOINT ["/server"]
