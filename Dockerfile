FROM golang:1.27-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd ./cmd
COPY internal ./internal
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/ragd ./cmd/ragd

FROM alpine:3.22
RUN apk add --no-cache ca-certificates \
 && adduser -D -u 10001 app \
 && mkdir -p /data && chown app:app /data
COPY --from=build /out/ragd /usr/local/bin/ragd
COPY eval /app/eval
WORKDIR /app
USER app
ENV RAG_DATA=/data RAG_ADDR=0.0.0.0:8090
VOLUME ["/data"]
EXPOSE 8090
HEALTHCHECK --interval=30s --timeout=3s CMD wget -qO- http://127.0.0.1:8090/healthz || exit 1
ENTRYPOINT ["ragd"]
CMD ["serve"]
