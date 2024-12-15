FROM golang:1.20 AS builder
WORKDIR /
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN go build -o main .

FROM debian:bullseye-slim
ENV PORT=8000
WORKDIR /
COPY --from=builder /app/main .
EXPOSE 8080
CMD ["./main"]
