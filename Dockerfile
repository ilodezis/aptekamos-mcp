# Builder: static Go binary
FROM golang:1.27-alpine AS service
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY *.go ./
COPY internal ./internal
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/aptekamos-mcp .

# Runtime: CA certificates and static binary
FROM gcr.io/distroless/static-debian13:nonroot
WORKDIR /app
COPY --from=service /out/aptekamos-mcp /app/aptekamos-mcp
ENV ADDR=:8130
USER 1000:1000
EXPOSE 8130
ENTRYPOINT ["/app/aptekamos-mcp"]
