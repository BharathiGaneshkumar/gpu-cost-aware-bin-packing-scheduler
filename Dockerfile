# --- Stage 1: build ---
# Full Go toolchain, only used to compile. Thrown away after this stage.
FROM golang:1.26 AS builder

WORKDIR /src

# Copy go.mod/go.sum first and download deps -- separate layer so Docker
# can cache this step and skip re-downloading deps if only source changes.
COPY go.mod go.sum ./
RUN go mod download

# Now copy the actual source code and build.
COPY . .
# CGO_ENABLED=0 -> static binary, no C library dependency, works on the
# minimal base image in stage 2 with nothing else installed.
RUN CGO_ENABLED=0 GOOS=linux go build -o /device-plugin ./cmd/device-plugin

# --- Stage 2: final, minimal image ---
FROM alpine:latest

COPY --from=builder /device-plugin /device-plugin

ENTRYPOINT ["/device-plugin"]