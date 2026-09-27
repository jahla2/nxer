FROM golang:1.26.8-alpine AS build

WORKDIR /src
RUN apk add --no-cache ca-certificates git

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/nexora-gateway .

FROM alpine:3.22

RUN apk add --no-cache ca-certificates     && addgroup -S nexora     && adduser -S -G nexora -H -s /sbin/nologin nexora

COPY --from=build /out/nexora-gateway /usr/local/bin/nexora-gateway

USER nexora
EXPOSE 8080

ENV GATEWAY_ADDR=:8080
ENTRYPOINT ["/usr/local/bin/nexora-gateway"]
