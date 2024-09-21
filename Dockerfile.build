ARG GO_VERSION=1.23
ARG ALPINE_VERSION=3.20
FROM --platform=$BUILDPLATFORM golang:${GO_VERSION} as builder

WORKDIR /build

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o videohub_exporter .


FROM alpine:${ALPINE_VERSION}

RUN addgroup -g 998 appuser && \
    adduser -S -h /app -u 998 appuser

COPY --from=builder /build/videohub_exporter /app

WORKDIR /app
RUN chown appuser:appuser -R /app
USER appuser

EXPOSE 9990
ENTRYPOINT ["/app/videohub_exporter"]
