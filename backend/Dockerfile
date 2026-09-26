FROM golang:1.22-alpine

WORKDIR /app

# Install air for hot-reload in dev (optional but nice — remove for a leaner prod image)
RUN go install github.com/air-verse/air@latest

COPY go.mod ./
# go.sum is generated the first time you run `go mod tidy` locally — see backend/README.md
COPY go.sum* ./
RUN go mod download || true

COPY . .

EXPOSE 8080

CMD ["air", "-c", ".air.toml"]
