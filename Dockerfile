FROM golang:1.22-alpine AS build

WORKDIR /src

COPY go.mod ./
COPY cmd ./cmd
COPY internal ./internal
COPY web ./web

ENV CGO_ENABLED=0
RUN go build -trimpath -ldflags="-s -w" -o /out/eventmap ./cmd/server

FROM alpine:3.20

WORKDIR /app

COPY --from=build /out/eventmap /app/eventmap
COPY web /app/web

ENV PORT=8080
EXPOSE 8080

CMD ["/app/eventmap"]
