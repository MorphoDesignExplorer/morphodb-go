FROM golang:1.23

WORKDIR /app

COPY . ./

EXPOSE 8000

RUN go build -o /main-bin main

WORKDIR /

CMD ["./main-bin"]
