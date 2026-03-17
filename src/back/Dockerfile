FROM golang:1.22-alpine AS builder

WORKDIR /app

COPY go.mod ./
RUN go mod download

COPY . .
RUN go build -o server .

FROM alpine:3.19

WORKDIR /app
COPY --from=builder /app/server .

EXPOSE 3420

CMD ["./server"]
