# ttl — distroless container.
#
# Multi-stage build:
#   1) build the static binary using golang:alpine
#   2) copy into gcr.io/distroless/static-debian12 (no shell, no package mgr)
#
# Image size: ~20 MB.

FROM golang:1.25.12-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ARG VERSION=dev
ARG COMMIT=none
ARG DATE=unknown
RUN CGO_ENABLED=0 go build -trimpath \
    -ldflags="-s -w -X main.version=${VERSION} -X main.commit=${COMMIT} -X main.date=${DATE}" \
    -o /out/ttl ./cmd/ttl && \
    mkdir /out/data

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/ttl /usr/local/bin/ttl
COPY --from=build --chown=nonroot:nonroot /out/data /data
ENV TTL_DATA_DIR=/data
EXPOSE 8093
USER nonroot:nonroot
ENTRYPOINT ["/usr/local/bin/ttl"]
CMD ["serve", "--addr", ":8093", "--db", "/data/ttl.db"]
