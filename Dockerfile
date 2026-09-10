FROM golang:1.27-alpine AS builder

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o kiwivm-exporter .

FROM gcr.io/distroless/static-debian12:nonroot

COPY --from=builder /app/kiwivm-exporter /kiwivm-exporter

EXPOSE 9103

ENTRYPOINT ["/kiwivm-exporter"]
CMD ["--config-path", "/etc/kiwivm/config.yml"]
