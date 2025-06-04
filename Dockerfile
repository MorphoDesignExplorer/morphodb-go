# syntax=docker/dockerfile:1
FROM golang:1.23-alpine

WORKDIR /app

# copy folder contents into image
COPY . ./

RUN go build -o /bin/main-bin server.go

RUN go build -o /bin/user-manager user-manager/main.go

WORKDIR /

CMD ["/bin/main-bin"]

