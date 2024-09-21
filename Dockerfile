ARG ALPINE_VERSION=3.20

FROM --platform=$BUILDPLATFORM alpine:${ALPINE_VERSION}

RUN addgroup -g 998 appuser && \
    adduser -S -h /app -u 998 appuser

# Copy built binary by goreleaser
COPY videohub_exporter /app

WORKDIR /app
RUN chown appuser:appuser -R /app
USER appuser

EXPOSE 9990
ENTRYPOINT ["/app/videohub_exporter"]