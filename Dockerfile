FROM golang:1.23.2 AS build
WORKDIR /app
COPY . .
RUN go build -o main main.go
CMD ["./main"]