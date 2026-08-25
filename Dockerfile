# syntax=docker/dockerfile:1
FROM golang:1.22-alpine AS build
WORKDIR /src
COPY go.mod ./
COPY cmd ./cmd
COPY mcp ./mcp
COPY saga ./saga
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /semantic-saga-mcp ./cmd/semantic-saga-mcp

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /semantic-saga-mcp /semantic-saga-mcp
EXPOSE 8080
USER nonroot:nonroot
ENTRYPOINT ["/semantic-saga-mcp"]
