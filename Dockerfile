FROM golang:1.25.5-alpine AS builder

WORKDIR /build
COPY . .
RUN go build -ldflags="-s -w" -o jotes .

FROM alpine:latest

# Install ripgrep because all unified filename/content search modes rely on rg.
RUN apk add --no-cache ripgrep

# Configure a writable runtime data directory so the container works by default.
ENV JOTES_DATA_DIR=/var/lib/jotes

RUN adduser -D -H jotes && mkdir -p /var/lib/jotes && chown jotes:jotes /var/lib/jotes
USER jotes

COPY --from=builder /build/jotes /usr/local/bin/jotes

EXPOSE 7887

ENTRYPOINT ["jotes"]
