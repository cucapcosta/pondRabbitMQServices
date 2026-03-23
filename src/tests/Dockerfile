FROM golang:1.23 AS builder

WORKDIR /app

COPY go.mod go.sum ./
RUN GOPROXY=direct GONOSUMDB=* GONOSUMCHECK=* go mod download

COPY . .
RUN CGO_ENABLED=0 go build -o loadtest .

FROM alpine:3.21

WORKDIR /app
COPY --from=builder /app/loadtest .

CMD ["./loadtest"]
