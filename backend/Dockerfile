FROM golang:1.22-alpine AS build
WORKDIR /src
RUN apk add --no-cache git
COPY go.mod ./
RUN go mod download || true
COPY . .
RUN go mod tidy

# Server binary (Linux amd64, runs inside this container)
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags="-s -w" -o /out/server ./cmd/server

# Cross-compile the agent for all common platforms.
# These get bundled into the runtime image so the admin UI can offer them as downloads.
RUN mkdir -p /agents && \
    CGO_ENABLED=0 GOOS=linux   GOARCH=amd64 go build -trimpath -ldflags="-s -w" -o /agents/face_auth-agent-linux-amd64        ./cmd/agent && \
    CGO_ENABLED=0 GOOS=linux   GOARCH=arm64 go build -trimpath -ldflags="-s -w" -o /agents/face_auth-agent-linux-arm64        ./cmd/agent && \
    CGO_ENABLED=0 GOOS=linux   GOARCH=arm   GOARM=7 go build -trimpath -ldflags="-s -w" -o /agents/face_auth-agent-linux-armv7 ./cmd/agent && \
    CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -trimpath -ldflags="-s -w" -o /agents/face_auth-agent-windows-amd64.exe  ./cmd/agent && \
    CGO_ENABLED=0 GOOS=darwin  GOARCH=amd64 go build -trimpath -ldflags="-s -w" -o /agents/face_auth-agent-darwin-amd64       ./cmd/agent && \
    CGO_ENABLED=0 GOOS=darwin  GOARCH=arm64 go build -trimpath -ldflags="-s -w" -o /agents/face_auth-agent-darwin-arm64       ./cmd/agent

FROM alpine:3.19
RUN apk add --no-cache ca-certificates tzdata
WORKDIR /app
COPY --from=build /out/server /app/server
COPY --from=build /agents /app/agents
EXPOSE 7660 8080
ENTRYPOINT ["/app/server"]
