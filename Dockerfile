# syntax=docker/dockerfile:1
FROM golang:1.23

WORKDIR /app

# copy folder contents into image
COPY . ./

# install mount-s3
RUN wget https://s3.amazonaws.com/mountpoint-s3-release/latest/x86_64/mount-s3.deb

RUN apt-get install -y ./mount-s3.deb

RUN rm ./mount-s3.db

# mount s3 bucket locally

RUN mkdir /morpho-temp/

RUN mount-s3 --read-only "morpho-temp" /morpho-temp/

# build go binary

RUN go build -o /bin/main-bin server.go

RUN go build -o /bin/user-manager user-manager/main.go

WORKDIR /

CMD ["/bin/main-bin"]

