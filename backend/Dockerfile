# Build stage: compile a static binary.
FROM golang:1.22-alpine AS build
WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o /out/server ./cmd/server

# Runtime stage: small, no build tools, non-root user.
FROM alpine:3.19
RUN apk add --no-cache ca-certificates && \
    addgroup -S app && adduser -S app -G app
WORKDIR /app
COPY --from=build /out/server /app/server
USER app

# The server reads PORT itself; EXPOSE is documentation for tooling.
EXPOSE 8080
ENTRYPOINT ["/app/server"]
