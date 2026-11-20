FROM --platform=$BUILDPLATFORM golang:1.27-alpine AS builder

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .
ARG TARGETOS TARGETARCH
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -ldflags="-s -w" -o kiwivm-exporter .

FROM gcr.io/distroless/static-debian12:nonroot@sha256:afa5c872c891853ca7fcf1f12c3edb23f7eeef36189728842dd51042ff57f7ab

COPY --from=builder /app/kiwivm-exporter /kiwivm-exporter

EXPOSE 9103

ENTRYPOINT ["/kiwivm-exporter"]
CMD ["--config-path", "/etc/kiwivm/config.yml"]
