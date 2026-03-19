FROM golang:1.22 AS builder

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 go build -o server .

FROM alpine:3.21

WORKDIR /app
COPY --from=builder /app/server .

EXPOSE 3420

CMD ["./server"]
