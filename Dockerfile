FROM golang:1.21.4 AS builder

WORKDIR /app

COPY go.mod go.sum ./

RUN go mod download

COPY . .

RUN CGO_ENABLED=0 go build -o main .

FROM alpine:latest

RUN addgroup -g 988 -S pterodactyl && adduser -u 988 -S -G pterodactyl pterodactyl

WORKDIR /home/pterodactyl

USER pterodactyl

COPY --from=builder /app/main .

CMD ["./main"]
