FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/monitor ./cmd/monitor \
    && mkdir /data && chown 65532:65532 /data

FROM scratch
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --chown=65532:65532 --from=build /data /data
COPY --from=build /out/monitor /monitor
USER 65532:65532
ENV DATABASE_PATH=/data/monitor.db HTTP_LISTEN_ADDRESS=0.0.0.0:8080 PROVIDER_MODE=mock
EXPOSE 8080
STOPSIGNAL SIGTERM
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 CMD ["/monitor", "healthcheck"]
ENTRYPOINT ["/monitor"]
