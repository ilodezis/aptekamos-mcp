# Builder: static Go binary
FROM golang:1.27.3-alpine@sha256:d200d2dd76b03a99b52651fd1325aff7dcd35422c35c01bac56571b0507f3deb AS service
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY *.go ./
COPY internal ./internal
ARG GIT_SHA=dev
ARG BUILD_DATE=unknown
RUN CGO_ENABLED=0 go build -trimpath \
    -ldflags="-s -w -X github.com/ilodezis/aptekamos-mcp/internal/mcp.gitCommit=${GIT_SHA} -X github.com/ilodezis/aptekamos-mcp/internal/mcp.buildDate=${BUILD_DATE}" \
    -o /out/aptekamos-mcp .

# Runtime: CA certificates and static binary
FROM gcr.io/distroless/static-debian13:nonroot@sha256:18f4cc26c9ffa7f20a359298be7deadac5c607c8e03a02b8119f1a5fc2f82c99
WORKDIR /app
COPY --from=service /out/aptekamos-mcp /app/aptekamos-mcp
ENV ADDR=:8130
USER 1000:1000
EXPOSE 8130
ENTRYPOINT ["/app/aptekamos-mcp"]
