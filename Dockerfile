# syntax=docker/dockerfile:1
FROM golang:1.22-alpine AS build
WORKDIR /src
COPY go.mod ./
COPY cmd ./cmd
COPY gate ./gate
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /mcp-gate ./cmd/mcp-gate

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /mcp-gate /mcp-gate
EXPOSE 8080
USER nonroot:nonroot
ENTRYPOINT ["/mcp-gate"]
